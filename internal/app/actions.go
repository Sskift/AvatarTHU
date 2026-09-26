package app

import (
	"bufio"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

func same(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }
func decodeValue(v any) M {
	if m, ok := v.(M); ok {
		return m
	}
	if s, ok := v.(string); ok {
		var m M
		if json.Unmarshal([]byte(s), &m) == nil && m != nil {
			return m
		}
	}
	return M{}
}
func (a *App) verifiedFiles(st M) string {
	base := a.outputDir(st)
	items := append([]M{}, objects(st["artifacts"])...)
	items = append(items, M{"path": st["submission"], "sha256": st["sha256"]})
	for _, item := range items {
		file := inside(base, relative(base, str(item, "path")))
		ensure(same(digest(file), str(item, "sha256")), "产物已发生变化，请检查后使用新卡片")
	}
	return str(st, "submission")
}
func (a *App) submissionReceipt(st M) {
	if str(st, "status") != "submitted" || st["receipt_message"] != nil {
		return
	}
	st["receipt_message"] = a.send(receiptCard("作业提交成功", str(st, "title"), "网络学堂已返回成功回执。", true), fmt.Sprintf("submitted:%s:%d", str(st, "task_id"), number(st, "revision", 1)))
	a.saveTask(st)
}
func (a *App) act(event M) bool {
	if !a.enabled() || str(event, "operator_id") != str(a.config(), "lark_user_id") || str(event, "event_id") == "" {
		return false
	}
	value := decodeValue(event["action_value"])
	if str(value, "action") == "diagnostic" {
		defer a.lock("diagnostic", true)()
		p := a.data("diagnostic.json")
		st := readMap(p)
		if str(st, "message_id") == "" || str(event, "message_id") != str(st, "message_id") || !same(str(value, "nonce"), str(st, "nonce")) || str(st, "confirmed_at") != "" {
			return false
		}
		st["confirmed_at"] = stamp()
		st["event_id"] = event["event_id"]
		writeJSON(p, st)
		a.send(receiptCard("按钮连接正常", "飞书已连接到本机", "你的按钮操作已收到，作业完成后会在当前版本卡片等待决定。", true), "diagnostic:"+str(event, "event_id"))
		return true
	}
	tid, action := str(value, "task_id"), str(value, "action")
	if str(event, "action_name") == "revise" {
		matches := []M{}
		for _, st := range a.tasks() {
			if str(st, "card_message_id") != "" && str(st, "card_message_id") == str(event, "message_id") {
				matches = append(matches, st)
			}
		}
		if len(matches) != 1 {
			return false
		}
		tid = str(matches[0], "task_id")
		action = "revise"
	}
	if action != "submit" && action != "revise" && action != "revise_comments" {
		return false
	}
	if !regexp.MustCompile(`^[a-f0-9]{16}$`).MatchString(tid) {
		return false
	}
	defer a.lock(tid, true)()
	st := readMap(a.taskPath(tid))
	switch str(st, "status") {
	case "awaiting", "needs_student", "approval_invalid":
	default:
		return false
	}
	if str(event, "message_id") == "" || str(event, "message_id") != str(st, "card_message_id") {
		return false
	}
	if action == "revise" || action == "revise_comments" {
		feedback := ""
		if action == "revise" {
			feedback = strings.TrimSpace(str(decodeValue(event["form_value"]), "feedback"))
			if feedback == "" {
				return false
			}
		} else {
			if number(value, "revision", -1) != number(st, "revision", 0) || !same(str(value, "nonce"), str(st, "nonce")) || !boolean(obj(st, "review_doc"), "verified") {
				return false
			}
			entries := a.comments(st)
			if len(entries) == 0 {
				a.send(receiptCard("尚未发现可处理批注", str(st, "title"), "请在审阅文档添加文字批注后再点击，也可直接在卡片填写意见。", false), "no-comments:"+str(event, "event_id"))
				return false
			}
			parts := []string{}
			for _, e := range entries {
				parts = append(parts, "位置与上下文："+strDefault(e, "context", "以引用原文和意见定位")+"\n引用："+str(e, "quote")+"\n修改意见："+str(e, "text"))
			}
			feedback = strings.Join(parts, "\n\n")
			st["review_comments"] = entries
		}
		merge(st, M{"status": "revision_ready", "feedback": feedback, "approval_event": nil})
		a.saveTask(st)
		a.send(receiptCard("已收到修改意见", str(st, "title"), "已排入本地队列，完成后发送新版审阅卡片。", true), "feedback:"+str(event, "event_id"))
		return true
	}
	if !boolean(st, "ready") || str(st, "status") != "awaiting" || len(texts(st["blockers"])) > 0 || number(value, "revision", -1) != number(st, "revision", 0) || str(st, "nonce") == "" || !same(str(value, "nonce"), str(st, "nonce")) {
		return false
	}
	if str(value, "sha256") != "" && !same(str(value, "sha256"), str(st, "sha256")) {
		return false
	}
	var file string
	var school *School
	e := attempt(func() { file = a.verifiedFiles(st); school = a.verifyOpen(st) })
	if e != nil {
		merge(st, M{"status": "approval_invalid", "error": safeError(e)})
		a.saveTask(st)
		a.send(receiptCard("未提交作业", str(st, "title"), safeError(e), false), "invalid:"+str(event, "event_id"))
		return false
	}
	merge(st, M{"status": "submitting", "approval_event": event["event_id"], "approved_at": stamp()})
	a.saveTask(st)
	var receipt M
	e = attempt(func() {
		if a.Upload != nil {
			receipt = a.Upload(school, st, file)
		} else {
			receipt = uploadApproved(school, st, file)
		}
	})
	if e != nil {
		merge(st, M{"status": "submission_unknown", "error": safeError(e)})
		a.saveTask(st)
		a.send(receiptCard("提交结果待核对", str(st, "title"), "未得到成功回执，请在网络学堂核对，系统不会自动重试。", false), "unknown:"+str(event, "event_id"))
		return false
	}
	merge(st, M{"status": "submitted", "submission_receipt": receipt, "submitted_at": stamp()})
	a.saveTask(st)
	a.submissionReceipt(st)
	return true
}

// The sole real submission call. Its only caller is the owner/card/version-bound action above.
func uploadApproved(s *School, st M, file string) M {
	f, e := os.Open(file)
	check(e)
	defer f.Close()
	tmp, e := os.CreateTemp(filepath.Dir(file), ".upload-")
	check(e)
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	form := multipart.NewWriter(tmp)
	part, e := form.CreateFormFile("fileupload", filepath.Base(file))
	check(e)
	_, e = io.Copy(part, f)
	check(e)
	for k, v := range map[string]string{"xszyid": str(st, "xszyid"), "isDeleted": "0", "zynr": "经本人飞书确认提交"} {
		check(form.WriteField(k, v))
	}
	check(form.Close())
	_, e = tmp.Seek(0, 0)
	check(e)
	r := s.request("POST", "/b/wlxt/kczy/zy/student/tjzy", nil, tmp, form.FormDataContentType(), true)
	defer r.Body.Close()
	var response M
	check(json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(&response))
	ensure(str(response, "result") == "success", "网络学堂未确认成功，请在网页核对")
	return M{"result": "success", "received_at": stamp()}
}
func (a *App) message(event M) bool {
	if !a.enabled() || str(event, "sender_id") != str(a.config(), "lark_user_id") || str(event, "message_id") == "" {
		return false
	}
	content := str(event, "content")
	if value := decodeValue(event["content"]); len(value) > 0 {
		content = str(value, "text")
	}
	reply := strDefault(event, "reply_to", str(event, "root_id"))
	tid, feedback := "", strings.TrimSpace(content)
	if matches := regexp.MustCompile(`(?s)^意见\s+([a-f0-9]{16})\s+(.+)$`).FindStringSubmatch(feedback); len(matches) > 0 {
		tid, feedback = matches[1], matches[2]
	} else {
		matches := []M{}
		for _, st := range a.tasks() {
			if reply != "" && str(st, "card_message_id") == reply {
				matches = append(matches, st)
			}
		}
		if len(matches) != 1 {
			return false
		}
		tid = str(matches[0], "task_id")
	}
	if feedback == "" {
		return false
	}
	defer a.lock(tid, true)()
	st := readMap(a.taskPath(tid))
	for _, m := range texts(st["feedback_messages"]) {
		if m == str(event, "message_id") {
			return false
		}
	}
	switch str(st, "status") {
	case "awaiting", "needs_student", "revision_requested", "approval_invalid":
	default:
		return false
	}
	if str(st, "chat_id") != "" && str(st, "chat_id") != str(event, "chat_id") {
		return false
	}
	st["feedback_messages"] = append(texts(st["feedback_messages"]), str(event, "message_id"))
	merge(st, M{"status": "revision_ready", "feedback": feedback, "approval_event": nil})
	a.saveTask(st)
	a.send(receiptCard("修改已排队", str(st, "title"), "会自动处理并独立复审，完成后发给你。", true), "feedback:"+str(event, "message_id"))
	return true
}
func (a *App) consume(ctx context.Context, key, name string, handler func(M) bool) {
	exe := findExecutable("lark-cli", str(a.config(), "lark_cli"))
	ensure(exe != "", "未找到 Lark CLI")
	// Closing stdin lets Lark unsubscribe cleanly before forced process cleanup.
	cmd := command(context.WithoutCancel(ctx), append([]string{exe}, a.larkArgs("event", "consume", key, "--as", "bot")...)...)
	cmd.Dir = a.Root
	input, e := cmd.StdinPipe()
	check(e)
	defer input.Close()
	out, e := cmd.StdoutPipe()
	check(e)
	errout, e := cmd.StderrPipe()
	check(e)
	check(cmd.Start())
	defer bindProcess(cmd)()
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-done:
			return
		case <-ctx.Done():
			_ = input.Close()
		}
		select {
		case <-done:
			return
		case <-time.After(5 * time.Second):
			_ = cmd.Cancel()
		}
		select {
		case <-done:
			return
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
		}
	}()
	health := a.data(name + "-health.json")
	starting := readMap(health)
	merge(starting, M{"state": "starting", "pid": cmd.Process.Pid, "time": stamp()})
	writeJSON(health, starting)
	var diagnostics strings.Builder
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if e := attempt(func() {
			sc := bufio.NewScanner(errout)
			sc.Buffer(make([]byte, 65536), 4<<20)
			for sc.Scan() {
				line := sc.Text()
				if diagnostics.Len() > 32768 {
					kept := diagnostics.String()
					diagnostics.Reset()
					diagnostics.WriteString(kept[len(kept)-16384:])
				}
				diagnostics.WriteString(line + "\n")
				if strings.Contains(line, "[event] ready ") {
					writeJSON(health, M{"state": "ready", "pid": cmd.Process.Pid, "time": stamp(), "last_ready_at": stamp(), "consecutive_failures": 0})
				}
				fmt.Fprintln(os.Stderr, safeError(line))
			}
		}); e != nil {
			fmt.Fprintln(os.Stderr, safeError(e))
		}
	}()
	scanner := bufio.NewScanner(out)
	scanner.Buffer(make([]byte, 65536), 16<<20)
	for scanner.Scan() {
		line := append([]byte{}, scanner.Bytes()...)
		if e := attempt(func() {
			event := parseMap(line)
			if handler(event) {
				writeJSON(a.data(name+"-last-event.json"), M{"event_id": event["event_id"], "message_id": event["message_id"], "type": event["type"], "time": stamp()})
			}
		}); e != nil {
			fmt.Fprintln(os.Stderr, "事件处理失败："+safeError(e))
		}
	}
	scanErr := scanner.Err()
	e = cmd.Wait()
	wg.Wait()
	if ctx.Err() == nil {
		check(scanErr)
		if e == nil {
			e = fmt.Errorf("Lark 事件连接结束")
		}
		panic(fmt.Errorf("%s: %w", safeError(diagnostics.String()), e))
	}
	stopped := readMap(health)
	delete(stopped, "pid")
	merge(stopped, M{"state": "stopped", "time": stamp()})
	writeJSON(health, stopped)
}
