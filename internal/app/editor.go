package app

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"golang.org/x/net/html"
)

//go:embed editor_ui.html editor_ui.css editor_ui.js workspace_ui.html workspace_ui.css workspace_ui.js pdfjs
var editorUI embed.FS

var editorRequestID = regexp.MustCompile(`^[a-f0-9]{32}$`)
var reportMarkdown = goldmark.New(goldmark.WithExtensions(extension.GFM))

func (a *App) editorDir(st M) string {
	return filepath.Join(strDefault(st, "assignment_dir", a.data("jobs", str(st, "task_id"))), "editor")
}
func (a *App) editorDraft(st M) M {
	path := filepath.Join(a.editorDir(st), "draft.json")
	if d := readMap(path); len(d) != 0 {
		return d
	}
	return a.seedEditor(st)
}
func (a *App) seedEditor(st M) M {
	root := a.editorDir(st)
	previous := readMap(filepath.Join(root, "draft.json"))
	revision := number(st, "revision", 1)
	if n, e := strconv.Atoi(strings.TrimPrefix(filepath.Base(a.outputDir(st)), "r")); e == nil && n > 0 {
		revision = n
	}
	source := filepath.Join(root, "sources", "r"+fmt.Sprint(revision)+"-"+randomID(4))
	mkdir(source)
	var markdown string
	existing := filepath.Join(str(st, "assignment_dir"), "report-source", fmt.Sprintf("r%d", revision))
	candidates, _ := filepath.Glob(filepath.Join(existing, "*.md"))
	if len(candidates) > 0 {
		markdown = string(readBytes(candidates[0]))
		copyEditorImages(existing, source)
	} else {
		for _, artifact := range objects(st["artifacts"]) {
			p := str(artifact, "path")
			if filepath.Base(p) == "report-source.zip" && exists(p) {
				extractEditorSource(p, source)
			}
		}
		candidates, _ = filepath.Glob(filepath.Join(source, "*.md"))
		if len(candidates) > 0 {
			markdown = string(readBytes(candidates[0]))
		} else {
			for _, artifact := range objects(st["artifacts"]) {
				p := str(artifact, "path")
				if strings.EqualFold(filepath.Ext(p), ".md") && filepath.Base(p) != "review.md" && exists(p) {
					markdown = string(readBytes(p))
					break
				}
			}
		}
	}
	ensure(len(markdown) <= 2<<20, "报告源文件超过 2 MB，请先缩小文件")
	d := M{"markdown": markdown, "base_markdown": markdown, "base_revision": revision, "version": number(previous, "version", 0) + 1, "source_dir": source, "updated_at": stamp()}
	writeJSON(filepath.Join(root, "draft.json"), d)
	return d
}
func readBytes(p string) []byte { b, e := os.ReadFile(p); check(e); return b }
func editorImage(p string) bool {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		return true
	}
	return false
}
func copyEditorImages(src, dst string) {
	if src == "" {
		return
	}
	for _, p := range visibleFiles(src) {
		if editorImage(p) {
			copyFile(inside(src, relative(src, p)), filepath.Join(dst, relative(src, p)))
		}
	}
}
func extractEditorSource(p, dst string) {
	z, e := zip.OpenReader(p)
	check(e)
	defer z.Close()
	var total int64
	ensure(len(z.File) <= 500, "报告源文件压缩包中的文件过多")
	for _, f := range z.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name := filepath.ToSlash(f.Name)
		ensure(!strings.HasPrefix(name, "/") && !strings.ContainsAny(name, "\\:") && f.Mode()&os.ModeSymlink == 0, "报告源文件路径无效")
		for _, part := range strings.Split(name, "/") {
			ensure(part != "" && !strings.HasPrefix(part, "."), "报告源文件路径无效")
		}
		if !editorImage(name) && !strings.HasSuffix(strings.ToLower(name), ".md") {
			continue
		}
		r, e := f.Open()
		check(e)
		b, e := io.ReadAll(io.LimitReader(r, 32<<20+1))
		r.Close()
		check(e)
		total += int64(len(b))
		ensure(len(b) <= 32<<20 && total <= 100<<20, "报告源文件过大")
		writeFile(filepath.Join(dst, filepath.FromSlash(name)), b, 0600)
	}
}
func editorEditable(st M) bool {
	switch str(st, "status") {
	case "awaiting", "needs_student", "approval_invalid":
		return true
	}
	return false
}
func (a *App) editorRequests(st M) []M {
	paths, _ := filepath.Glob(filepath.Join(a.editorDir(st), "requests", "*", "request.json"))
	result := []M{}
	for _, p := range paths {
		r := readMap(p)
		if str(r, "kind") == "submit" && str(obj(st, "editor_submission"), "id") == str(r, "id") {
			r["task_status"] = st["status"]
			r["task_revision"] = st["revision"]
			r["task_error"] = st["error"]
			r["review_outcome"] = st["review_outcome"]
		}
		delete(r, "base_markdown")
		delete(r, "plan")
		result = append(result, r)
	}
	sort.Slice(result, func(i, j int) bool { return str(result[i], "created_at") > str(result[j], "created_at") })
	if len(result) > 30 {
		result = result[:30]
	}
	return result
}
func editorTaskSummary(st M) M {
	return M{"id": st["task_id"], "title": st["title"], "course": st["course"], "revision": st["revision"], "status": st["status"], "error": st["error"], "can_submit": editorEditable(st), "document_url": obj(st, "review_doc")["url"]}
}
func (a *App) editorView(st M) M {
	d := a.editorDraft(st)
	public := M{}
	for _, k := range []string{"markdown", "base_markdown", "base_revision", "version", "updated_at"} {
		public[k] = d[k]
	}
	artifacts := []string{}
	for _, item := range objects(st["artifacts"]) {
		artifacts = append(artifacts, filepath.Base(str(item, "path")))
	}
	plan := a.pairing()
	return M{"task": editorTaskSummary(st), "draft": public, "requests": a.editorRequests(st), "artifacts": artifacts, "writer": plan["writer"], "reviewer": plan["reviewer"]}
}
func utf16Slice(s string, start, end int) string {
	u := utf16.Encode([]rune(s))
	ensure(start >= 0 && end >= start && end <= len(u), "选区已失效，请重新选择")
	for _, i := range []int{start, end} {
		ensure(i == 0 || i == len(u) || !(u[i] >= 0xdc00 && u[i] <= 0xdfff), "选区不能截断字符")
	}
	return string(utf16.Decode(u[start:end]))
}
func (a *App) saveEditorDraft(st, in M) M {
	d := a.editorDraft(st)
	ensure(number(in, "version", 0) == number(d, "version", 1), "另一页面已保存了修改。请下载当前文字备份，然后刷新页面。")
	md, ok := in["markdown"].(string)
	ensure(ok && len(md) <= 2<<20, "报告正文应小于 2 MB")
	merge(d, M{"markdown": md, "version": number(d, "version", 1) + 1, "updated_at": stamp()})
	writeJSON(filepath.Join(a.editorDir(st), "draft.json"), d)
	return d
}
func (a *App) newEditorRequest(st, in M, kind string) M {
	id := str(in, "id")
	ensure(editorRequestID.MatchString(id), "无效请求编号")
	dir := filepath.Join(a.editorDir(st), "requests", id)
	if r := readMap(filepath.Join(dir, "request.json")); len(r) > 0 {
		return r
	}
	d := a.editorDraft(st)
	ensure(number(in, "version", 0) == number(d, "version", 1), "草稿已有变化，请等待保存后重新操作")
	ensure(number(d, "base_revision", 0) == number(st, "revision", 1), "已有新版产物。请先载入新版，再合并你的修改。旧草稿会保留备份。")
	r := M{"id": id, "kind": kind, "state": "queued", "created_at": time.Now().UTC().Format(time.RFC3339Nano), "base_revision": st["revision"], "draft_version": d["version"], "instruction": strings.TrimSpace(str(in, "instruction")), "plan": a.pairing()}
	ensure(len(str(r, "instruction")) <= 12000, "修改要求过长")
	if kind == "rewrite" {
		md := str(d, "markdown")
		if _, multiple := in["ranges"]; multiple {
			segments := editorSegments(md, objects(in["ranges"]))
			r["segments"] = segments
		} else {
			start, end := number(in, "start", -1), number(in, "end", -1)
			selected := utf16Slice(md, start, end)
			ensure(strings.TrimSpace(selected) != "" && len(selected) <= 32000, "请选择正文（合计最多 32 KB）")
			merge(r, M{"start": start, "end": end, "selected": selected})
		}
		ensure(str(r, "instruction") != "", "请说明选中的内容怎么改")
		a.ensureEditorAvailable(st)
		r["base_markdown"] = md
		copyEditorImages(str(d, "source_dir"), filepath.Join(dir, "manuscript"))
	} else {
		ensure(editorEditable(st), "当前作业正在处理或已提交，暂时不能接收定稿")
		ensure(strings.TrimSpace(str(d, "markdown")) != "", "报告正文不能为空")
		snapshot := filepath.Join(dir, "manuscript")
		copyEditorImages(str(d, "source_dir"), snapshot)
		writeFile(filepath.Join(snapshot, "report.md"), []byte(str(d, "markdown")), 0600)
		r["target_revision"] = number(st, "revision", 1) + 1
	}
	writeJSON(filepath.Join(dir, "request.json"), r)
	if kind == "submit" {
		a.acceptEditorFinal(st, r)
	}
	a.wakeEditor()
	return r
}
func (a *App) acceptEditorFinal(st, r M) {
	if str(obj(st, "editor_submission"), "id") == str(r, "id") {
		return
	}
	ensure(editorEditable(st) && number(st, "revision", 1) == number(r, "base_revision", 0), "作业状态已有变化，定稿未接收，请重新载入")
	merge(st, M{"status": "revision_ready", "feedback": "用户已在本地编辑页提交定稿。以 owner-final/report.md 为报告正文，保留用户措辞，更新 PDF、HTML、代码包内报告和相关说明；只调整受本次修改影响的文件。用户补充：" + str(r, "instruction"), "approval_event": nil, "editor_submission": M{"id": r["id"], "revision": r["target_revision"], "accepted_at": stamp()}})
	a.saveTask(st)
}
func (a *App) decideEditorSuggestion(st, in M) M {
	id := str(in, "id")
	ensure(editorRequestID.MatchString(id), "无效请求编号")
	p := filepath.Join(a.editorDir(st), "requests", id, "request.json")
	r := readMap(p)
	action := str(in, "action")
	ensure(action == "accept" || action == "dismiss", "无效操作")
	if str(r, "state") == "applied" || str(r, "state") == "dismissed" {
		return r
	}
	ensure(str(r, "kind") == "rewrite" && str(r, "state") == "ready", "此建议尚未完成")
	if action == "accept" {
		d := a.editorDraft(st)
		md := str(d, "markdown")
		ensure(number(d, "base_revision", 0) == number(r, "base_revision", -1), "此建议属于旧版报告，请重新提出修改")
		next := applyEditorReplacements(md, r)
		a.saveEditorDraft(st, M{"version": d["version"], "markdown": next})
		r["state"] = "applied"
	} else {
		r["state"] = "dismissed"
	}
	r["decided_at"] = stamp()
	writeJSON(p, r)
	return r
}
func (a *App) processEditorRequests(selected string) {
	for _, old := range a.tasks() {
		tid := str(old, "task_id")
		if selected != "" && tid != selected {
			continue
		}
		paths, _ := filepath.Glob(filepath.Join(a.editorDir(old), "requests", "*", "request.json"))
		for _, p := range paths {
			if a.Ctx.Err() != nil {
				return
			}
			var r M
			func() {
				defer a.lock("editor-"+tid, true)()
				r = readMap(p)
				state := str(r, "state")
				if (state != "queued" && state != "running") || parseTime(str(r, "retry_at")).After(time.Now()) {
					r = nil
					return
				}
				if str(r, "kind") == "rewrite" {
					if state == "running" {
						r["recovered_at"] = stamp()
					}
					previous := number(r, "attempt", 0)
					if previous == 0 && exists(filepath.Join(filepath.Dir(p), "job", "process.json")) {
						previous = 1
					}
					merge(r, M{"state": "running", "stage": "准备报告和资料", "started_at": stamp(), "attempt": previous + 1, "error": "", "retry_at": "", "notice": ""})
					writeJSON(p, r)
				}
			}()
			if r == nil {
				continue
			}
			e := attempt(func() {
				if str(r, "kind") == "submit" {
					defer a.lock(tid, true)()
					st := readMap(a.taskPath(tid))
					a.acceptEditorFinal(st, r)
					r["state"] = "accepted"
					return
				}
				st := readMap(a.taskPath(tid))
				ensure(number(st, "revision", 1) == number(r, "base_revision", 0), "已有新版产物，请载入新版后重新选择修改段落")
				plan := M{}
				merge(plan, obj(r, "plan"))
				plan["stage_timeout"] = min(number(plan, "stage_timeout", 7200), 600)
				job := filepath.Join(filepath.Dir(p), fmt.Sprintf("job-attempt-%d", number(r, "attempt", 1)))
				r["job"] = job
				mkdir(job)
				a.prepare(M{"description": st["description"], "folder": st["folder"], "courseware": st["courseware"]}, job)
				for _, artifact := range objects(st["artifacts"]) {
					source := str(artifact, "path")
					copyFile(source, filepath.Join(job, "current-artifacts", filepath.Base(source)))
				}
				copyEditorImages(filepath.Join(filepath.Dir(p), "manuscript"), job)
				writeFile(filepath.Join(job, "report.md"), []byte(str(r, "base_markdown")), 0600)
				merge(r, M{"stage": "Agent 正在核对资料并修改文字", "timeout_seconds": plan["stage_timeout"]})
				writeJSON(p, r)
				prompt, schema := editorRewritePrompt(r)
				result := a.engine(str(plan, "writer"), job, prompt, schema, "局部修改", plan)
				storeEditorReplacements(r, result)
				merge(r, M{"state": "ready", "summary": str(result, "summary"), "completed_at": stamp()})
			})
			if e != nil {
				a.failEditorRequest(r, e)
			}
			func() {
				defer a.lock("editor-"+tid, true)()
				writeJSON(p, r)
				if job := str(r, "job"); job != "" {
					writeJSON(filepath.Join(job, "request-result.json"), r)
				}
			}()
		}
	}
}
func (a *App) failEditorRequest(r M, err error) {
	if str(r, "kind") == "rewrite" && errors.Is(err, context.Canceled) {
		merge(r, M{"state": "queued", "stage": "等待后台恢复", "notice": "后台停止时修改被中断，重新启动后自动继续。", "interrupted_at": stamp(), "error": ""})
		return
	}
	failure := failureFields(err)
	message := safeError(err)
	if len(failure) > 0 {
		message = str(failure, "tool") + "：" + str(failure, "reason")
		if !boolean(failure, "retryable") {
			message += "。" + str(failure, "action")
		}
	}
	merge(r, M{"state": "error", "error": message, "failure": failure, "failed_at": stamp(), "failures": number(r, "failures", 0) + 1})
	category := str(failure, "category")
	if str(r, "kind") == "rewrite" && (category == "network" || category == "timeout") && boolean(failure, "retryable") && number(r, "failures", 0) < 3 {
		merge(r, M{"state": "queued", "stage": "等待重试", "retry_at": time.Now().Add(time.Minute).Format(time.RFC3339), "notice": "暂时未完成，后台会自动重试；最多连续尝试 3 次。"})
	}
}
func (a *App) ensureEditorAvailable(st M) {
	for _, r := range a.editorRequests(st) {
		ensure(str(r, "kind") != "rewrite" || (str(r, "state") != "queued" && str(r, "state") != "running"), "已有一条局部修改正在处理，请稍候")
	}
}
func (a *App) retryEditorRequest(st M, id string) M {
	ensure(editorRequestID.MatchString(id), "无效请求编号")
	p := filepath.Join(a.editorDir(st), "requests", id, "request.json")
	r := readMap(p)
	ensure(str(r, "kind") == "rewrite", "只能重试局部修改")
	if str(r, "state") == "queued" || str(r, "state") == "running" {
		return r
	}
	ensure(str(r, "state") == "error", "此请求不需要重试")
	d := a.editorDraft(st)
	ensure(number(r, "base_revision", -1) == number(st, "revision", 0) && number(d, "base_revision", -2) == number(r, "base_revision", -1), "已有新版产物，请载入新版后重新选择修改段落")
	a.ensureEditorAvailable(st)
	merge(r, M{"state": "queued", "stage": "等待 Agent", "error": "", "notice": "已重新排队，保留原来的修改要求。", "retry_at": "", "failures": 0, "retry_requested_at": stamp()})
	writeJSON(p, r)
	a.wakeEditor()
	return r
}
func (a *App) wakeEditor() {
	if a.editorWake != nil {
		select {
		case a.editorWake <- struct{}{}:
		default:
		}
	}
}

func editorHTML(md, tid string) string {
	var out bytes.Buffer
	check(reportMarkdown.Convert([]byte(md), &out))
	nodes, e := html.ParseFragment(bytes.NewReader(out.Bytes()), nil)
	check(e)
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			for i := range n.Attr {
				at := &n.Attr[i]
				if at.Key == "src" && n.Data == "img" {
					u, e := url.Parse(at.Val)
					if e != nil || u.IsAbs() || u.Host != "" || strings.HasPrefix(u.Path, "/") {
						at.Val = ""
					} else {
						at.Val = "/api/task/" + tid + "/asset?path=" + url.QueryEscape(u.Path)
					}
				}
				if at.Key == "href" {
					u, e := url.Parse(at.Val)
					if e != nil || (u.Scheme != "https" && u.Scheme != "http" && !strings.HasPrefix(at.Val, "#")) {
						at.Val = "#"
					}
				}
			}
			if n.Data == "a" {
				n.Attr = append(n.Attr, html.Attribute{Key: "target", Val: "_blank"}, html.Attribute{Key: "rel", Val: "noopener noreferrer"})
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	out.Reset()
	for _, n := range nodes {
		walk(n)
		check(html.Render(&out, n))
	}
	return out.String()
}
func editorCookieName(host string) string {
	return "avatarthu_editor_" + strings.ReplaceAll(host, ":", "_")
}
func (a *App) editorHandler(token, host string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'wasm-unsafe-eval'; style-src 'self'; img-src 'self' data:; font-src 'self' blob:; worker-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
		if r.Host != host {
			http.Error(w, "无效地址", http.StatusForbidden)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+host {
			http.Error(w, "无效来源", http.StatusForbidden)
			return
		}
		if r.URL.Path == "/session" && r.Method == "POST" {
			if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Editor-Token")), []byte(token)) != 1 {
				http.Error(w, "请通过 avatarthu edit 重新打开", 401)
				return
			}
			http.SetCookie(w, &http.Cookie{Name: editorCookieName(host), Value: token, Path: "/", MaxAge: 30 * 24 * 60 * 60, HttpOnly: true, SameSite: http.SameSiteStrictMode})
			w.WriteHeader(204)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/pdfjs/") && r.Method == "GET" {
			b, e := editorUI.ReadFile(strings.TrimPrefix(r.URL.Path, "/"))
			if e != nil {
				http.NotFound(w, r)
				return
			}
			contentType := "application/octet-stream"
			switch filepath.Ext(r.URL.Path) {
			case ".mjs", ".js":
				contentType = "text/javascript; charset=utf-8"
			case ".wasm":
				contentType = "application/wasm"
			case ".ttf":
				contentType = "font/ttf"
			}
			w.Header().Set("Content-Type", contentType)
			_, _ = w.Write(b)
			return
		}
		static := map[string]string{"/": "workspace_ui.html", "/workspace.css": "workspace_ui.css", "/workspace.js": "workspace_ui.js", "/editor": "editor_ui.html", "/editor.css": "editor_ui.css", "/editor.js": "editor_ui.js"}
		if file, ok := static[r.URL.Path]; ok && r.Method == "GET" {
			b, _ := editorUI.ReadFile(file)
			w.Header().Set("Content-Type", map[string]string{".html": "text/html; charset=utf-8", ".css": "text/css; charset=utf-8", ".js": "text/javascript; charset=utf-8"}[filepath.Ext(file)])
			_, _ = w.Write(b)
			return
		}
		c, e := r.Cookie(editorCookieName(host))
		if e != nil || subtle.ConstantTimeCompare([]byte(c.Value), []byte(token)) != 1 {
			http.Error(w, "请从菜单栏或 avatarthu edit 重新打开编辑页", 401)
			return
		}
		if a.workspaceAPI(w, r) {
			return
		}
		var result any
		e = attempt(func() {
			if r.URL.Path == "/api/tasks" && r.Method == "GET" {
				tasks := []M{}
				for _, st := range a.tasks() {
					tasks = append(tasks, editorTaskSummary(st))
				}
				result = tasks
				return
			}
			parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
			ensure(len(parts) >= 3 && parts[0] == "api" && parts[1] == "task", "无效请求")
			tid := parts[2]
			st := readMap(a.taskPath(tid))
			ensure(len(st) > 0, "找不到这份作业")
			action := ""
			if len(parts) == 4 {
				action = parts[3]
			}
			var in M
			if r.Method == "POST" {
				check(json.NewDecoder(http.MaxBytesReader(w, r.Body, 3<<20)).Decode(&in))
			} else {
				ensure(r.Method == "GET", "不支持的操作")
			}
			if action == "submit" && r.Method == "POST" {
				defer a.lock(tid, false)()
				st = readMap(a.taskPath(tid))
			}
			defer a.lock("editor-"+tid, true)()
			switch {
			case action == "" && r.Method == "GET":
				result = a.editorView(st)
			case action == "draft" && r.Method == "POST":
				a.saveEditorDraft(st, in)
				result = a.editorView(st)
			case action == "render" && r.Method == "POST":
				chunks := texts(in["blocks"])
				ensure(len(chunks) <= 600, "报告段落过多，请合并零碎段落")
				rendered := []string{}
				for _, chunk := range chunks {
					rendered = append(rendered, editorHTML(chunk, tid))
				}
				result = M{"html": editorHTML(str(in, "markdown"), tid), "blocks": rendered}
			case action == "rewrite" && r.Method == "POST":
				result = a.newEditorRequest(st, in, "rewrite")
			case action == "submit" && r.Method == "POST":
				result = a.newEditorRequest(st, in, "submit")
			case action == "decide" && r.Method == "POST":
				a.decideEditorSuggestion(st, in)
				result = a.editorView(st)
			case action == "retry" && r.Method == "POST":
				a.retryEditorRequest(st, str(in, "id"))
				result = a.editorView(st)
			case action == "reload" && r.Method == "POST":
				ensure(editorEditable(st), "新版产物尚未完成，请等处理结束后再载入")
				d := a.editorDraft(st)
				ensure(number(in, "version", 0) == number(d, "version", 1), "草稿已更新，请先保存")
				writeJSON(filepath.Join(a.editorDir(st), "backups", stampSafe()+"-"+randomID(4)+".json"), d)
				a.seedEditor(st)
				result = a.editorView(st)
			case action == "asset" && r.Method == "GET":
				d := a.editorDraft(st)
				name := r.URL.Query().Get("path")
				ensure(editorImage(name), "只支持报告图片")
				p := inside(str(d, "source_dir"), name)
				http.ServeFile(w, r, p)
				return
			case action == "download" && r.Method == "GET":
				d := a.editorDraft(st)
				w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
				w.Header().Set("Content-Disposition", `attachment; filename="report.md"`)
				_, _ = io.WriteString(w, str(d, "markdown"))
				return
			case action == "report.html" && r.Method == "GET":
				p := a.exportReportHTML(st)
				body := string(readBytes(p))
				w.Header().Set("Content-Security-Policy", reportPolicy+"; frame-ancestors 'none'")
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				if r.URL.Query().Get("download") == "1" {
					w.Header().Set("Content-Disposition", `attachment; filename="report.html"`)
				} else {
					body = strings.Replace(body, `<nav aria-label="报告目录">`, `<nav aria-label="报告目录"><a href="/editor#task=`+tid+`">← 返回编辑报告</a>`, 1)
				}
				_, _ = io.WriteString(w, body)
				return
			case action == "source.zip" && r.Method == "GET":
				d := a.editorDraft(st)
				var buffer bytes.Buffer
				z := zip.NewWriter(&buffer)
				entry, e := z.Create("report.md")
				check(e)
				_, e = io.WriteString(entry, str(d, "markdown"))
				check(e)
				for _, p := range visibleFiles(str(d, "source_dir")) {
					if editorImage(p) {
						entry, e = z.Create(filepath.ToSlash(relative(str(d, "source_dir"), p)))
						check(e)
						f, e := os.Open(p)
						check(e)
						_, e = io.Copy(entry, f)
						f.Close()
						check(e)
					}
				}
				check(z.Close())
				w.Header().Set("Content-Type", "application/zip")
				w.Header().Set("Content-Disposition", `attachment; filename="report-source.zip"`)
				_, _ = w.Write(buffer.Bytes())
				return
			default:
				panic(fmt.Errorf("无效请求"))
			}
		})
		if e != nil {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(409)
			_ = json.NewEncoder(w).Encode(M{"error": safeError(e)})
			return
		}
		if result != nil {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_ = json.NewEncoder(w).Encode(result)
		}
	})
}
func stampSafe() string { return time.Now().UTC().Format("20060102T150405") }
func (a *App) openEditor(tid string, noOpen bool) {
	a.openLocalWeb(tid, noOpen, false)
}
func (a *App) openLocalWeb(tid string, noOpen, workspace bool) {
	if tid != "" {
		ensure(len(readMap(a.taskPath(tid))) > 0, "找不到这份作业")
	}
	m := readMap(a.data("editor-server.json"))
	raw := str(m, "url")
	ensure(str(m, "state") != "unavailable", str(m, "error"))
	ensure(str(m, "state") == "running" && strings.HasPrefix(raw, "http://127.0.0.1:"), "编辑页尚未启动。请运行 avatarthu service start；原来的书签地址会继续生效。")
	u, e := url.Parse(raw)
	check(e)
	if !workspace {
		u.Path = "/editor"
		raw = u.String()
	}
	client := http.Client{Timeout: 2 * time.Second}
	resp, e := client.Get("http://" + u.Host + "/")
	ensure(e == nil, "后台暂时不可用，请运行 avatarthu service start")
	resp.Body.Close()
	if tid != "" {
		raw += "&task=" + tid
	}
	if noOpen {
		fmt.Println(raw)
		return
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("/usr/bin/open", raw)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", raw)
	default:
		cmd = exec.Command("xdg-open", raw)
	}
	check(cmd.Run())
	if workspace {
		fmt.Println("已打开 AvatarTHU 工作台。")
	} else {
		fmt.Println("已打开本地报告编辑页。")
	}
}
