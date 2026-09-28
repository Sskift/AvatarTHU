package app

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
)

func editorFixture(t *testing.T) (*App, M) {
	a := testApp(t)
	st := frozen(t, a)
	src := filepath.Join(str(st, "assignment_dir"), "report-source", "r1")
	writeFile(filepath.Join(src, "report.md"), []byte("# 报告\n\n原文🙂：先打开程序。\n\n![结果](screenshots/example.png)\n\n尾段保持。\n"), 0600)
	writeFile(filepath.Join(src, "screenshots", "example.png"), []byte("image"), 0600)
	return a, st
}
func editorCall(t *testing.T, a *App, path string, in M) *httptest.ResponseRecorder {
	t.Helper()
	method := "GET"
	var body io.Reader
	if in != nil {
		method = "POST"
		body = bytes.NewReader(jsonBytes(in))
	}
	req := httptest.NewRequest(method, "http://127.0.0.1:7777"+path, body)
	req.AddCookie(&http.Cookie{Name: editorCookieName("127.0.0.1:7777"), Value: "secret"})
	req.Header.Set("Origin", "http://127.0.0.1:7777")
	res := httptest.NewRecorder()
	a.editorHandler("secret", "127.0.0.1:7777").ServeHTTP(res, req)
	return res
}
func editorResult(t *testing.T, res *httptest.ResponseRecorder) M {
	t.Helper()
	if res.Code != 200 {
		t.Fatalf("%d: %s", res.Code, res.Body.String())
	}
	var m M
	check(json.Unmarshal(res.Body.Bytes(), &m))
	return m
}
func TestEditorAutosaveConflictAndSnapshot(t *testing.T) {
	a, st := editorFixture(t)
	root := a.editorDir(st)
	d := a.editorDraft(st)
	tid := str(st, "task_id")
	before := digest(str(st, "submission"))
	id := strings.Repeat("a", 32)
	md := strings.Replace(str(d, "markdown"), "先打开程序", "双击程序", 1)
	editorResult(t, editorCall(t, a, "/api/task/"+tid+"/draft", M{"version": 1, "markdown": md}))
	if r := editorCall(t, a, "/api/task/"+tid+"/draft", M{"version": 1, "markdown": "old tab"}); r.Code != 409 {
		t.Fatal("lost-update accepted")
	}
	editorResult(t, editorCall(t, a, "/api/task/"+tid+"/submit", M{"id": id, "version": 2, "instruction": "OWNER_NOTE"}))
	st = readMap(a.taskPath(tid))
	if str(st, "status") != "revision_ready" || number(st, "revision", 0) != 1 {
		t.Fatal(st)
	}
	editorResult(t, editorCall(t, a, "/api/task/"+tid+"/submit", M{"id": id, "version": 2}))
	if digest(str(st, "submission")) != before {
		t.Fatal("changed frozen submission")
	}
	editorResult(t, editorCall(t, a, "/api/task/"+tid+"/draft", M{"version": 2, "markdown": "后来继续修改"}))
	if string(readBytes(filepath.Join(root, "requests", id, "manuscript", "report.md"))) != md {
		t.Fatal("handoff changed with later draft")
	}
	if !exists(filepath.Join(root, "requests", id, "manuscript", "screenshots", "example.png")) {
		t.Fatal("missing image")
	}
	a.processEditorRequests("")
	if str(readMap(filepath.Join(root, "requests", id, "request.json")), "state") != "accepted" {
		t.Fatal("not accepted")
	}
	writers, reviews := 0, 0
	a.RunModel = func(engine, job, prompt string, schema M, role string, plan M) M {
		if role == "writer" {
			writers++
			if string(readBytes(filepath.Join(job, "owner-final", "report.md"))) != md {
				t.Fatal("writer did not receive frozen final")
			}
			if !strings.Contains(prompt, "OWNER_NOTE") {
				t.Fatal("missing owner note")
			}
			return resultAt(job, "2")
		}
		reviews++
		if _, e := os.Stat(filepath.Join(job, "owner-final")); e == nil {
			t.Fatal("reviewer received edit context")
		}
		for _, p := range visibleFiles(job) {
			b := string(readBytes(p))
			if strings.Contains(b, "OWNER_NOTE") || strings.Contains(b, "PRIVATE SELF ASSESSMENT") {
				t.Fatal("reviewer context contaminated")
			}
		}
		return M{"approved": true, "summary": "通过", "comments": []M{}, "checks": []string{"核对原题"}, "limitations": []string{}}
	}
	a.Upload = func(*School, M, string) M { t.Fatal("editor must not upload"); return nil }
	a.process(st)
	if writers != 1 || reviews != 1 || number(st, "revision", 0) != 2 || str(st, "status") != "awaiting" {
		t.Fatal(st)
	}
}
func TestEditorScopedRewriteAcceptAndConcurrentEdit(t *testing.T) {
	a, st := editorFixture(t)
	d := a.editorDraft(st)
	md := str(d, "markdown")
	selected := "原文🙂：先打开程序。"
	prefix := strings.Split(md, selected)[0]
	start := len(utf16.Encode([]rune(prefix)))
	in := M{"id": strings.Repeat("b", 32), "version": 1, "start": start, "end": start + len(utf16.Encode([]rune(selected))), "instruction": "直接说怎么用"}
	a.newEditorRequest(st, in, "rewrite")
	calls := 0
	a.RunModel = func(engine, job, prompt string, schema M, role string, plan M) M {
		calls++
		if role != "局部修改" || !strings.Contains(prompt, selected) {
			t.Fatal(prompt)
		}
		if _, e := os.Stat(filepath.Join(job, "owner-final")); e == nil {
			t.Fatal("unexpected context")
		}
		return M{"replacement_markdown": "双击程序开始。", "summary": "删去铺垫"}
	}
	a.processEditorRequests("")
	if calls != 1 || str(a.editorDraft(st), "markdown") != md {
		t.Fatal("suggestion auto-applied")
	}
	a.saveEditorDraft(st, M{"version": 1, "markdown": md + "\n我补充的另一段。"})
	a.decideEditorSuggestion(st, M{"id": in["id"], "action": "accept"})
	got := str(a.editorDraft(st), "markdown")
	if got != strings.Replace(md, selected, "双击程序开始。", 1)+"\n我补充的另一段。" {
		t.Fatal(got)
	}
	a.decideEditorSuggestion(st, M{"id": in["id"], "action": "accept"})
	if got != str(a.editorDraft(st), "markdown") {
		t.Fatal("duplicate apply")
	}
	if number(readMap(a.taskPath(str(st, "task_id"))), "revision", 0) != 1 {
		t.Fatal("local rewrite created artifact revision")
	}
}
func TestEditorRejectsChangedSelectionAndOldRevision(t *testing.T) {
	a, st := editorFixture(t)
	d := a.editorDraft(st)
	id := strings.Repeat("c", 32)
	p := filepath.Join(a.editorDir(st), "requests", id, "request.json")
	writeJSON(p, M{"id": id, "kind": "rewrite", "state": "ready", "selected": "先打开程序", "replacement": "双击程序", "base_revision": 1, "base_markdown": d["markdown"]})
	a.saveEditorDraft(st, M{"version": 1, "markdown": "我把这段完全重写了。"})
	expectError(t, "原文已改动", func() { a.decideEditorSuggestion(st, M{"id": id, "action": "accept"}) })
	st["revision"] = 2
	expectError(t, "已有新版", func() { a.newEditorRequest(st, M{"id": strings.Repeat("d", 32), "version": 2}, "submit") })
}
func TestEditorHTTPPrivacyAndTraversal(t *testing.T) {
	a, st := editorFixture(t)
	handler := a.editorHandler("secret", "127.0.0.1:7777")
	path := "/api/task/" + str(st, "task_id")
	for _, tc := range []struct {
		host, origin, cookie string
		want                 int
	}{{"evil.test", "", "secret", 403}, {"127.0.0.1:7777", "https://evil.test", "secret", 403}, {"127.0.0.1:7777", "", "wrong", 401}} {
		req := httptest.NewRequest("GET", "http://"+tc.host+path, nil)
		req.Header.Set("Origin", tc.origin)
		req.AddCookie(&http.Cookie{Name: editorCookieName("127.0.0.1:7777"), Value: tc.cookie})
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != tc.want {
			t.Fatal(res.Code)
		}
	}
	if r := editorCall(t, a, path+"/asset?path=../../config.json", nil); r.Code != 409 {
		t.Fatal(r.Code)
	}
	if r := editorCall(t, a, path+"/asset?path=screenshots/example.png", nil); r.Code != 200 || r.Body.String() != "image" {
		t.Fatal(r.Code, r.Body.String())
	}
	rendered := editorHTML("<script>alert(1)</script>\n\n![x](https://outside.test/x.png)\n\n![x](screenshots/example.png)", str(st, "task_id"))
	if strings.Contains(rendered, "<script>") || strings.Contains(rendered, `src="https://`) {
		t.Fatal(rendered)
	}
	if !strings.Contains(rendered, "/asset?path=screenshots%2Fexample.png") {
		t.Fatal(rendered)
	}
}
func TestEditorRestartAndSourceZip(t *testing.T) {
	a, st := editorFixture(t)
	id := strings.Repeat("e", 32)
	p := filepath.Join(a.editorDir(st), "requests", id, "request.json")
	d := a.editorDraft(st)
	r := a.newEditorRequest(st, M{"id": id, "version": 1, "start": 0, "end": 4, "instruction": "简短些"}, "rewrite")
	r["state"] = "running"
	writeJSON(p, r)
	writeJSON(filepath.Join(filepath.Dir(p), "job", "process.json"), M{"pid": -1})
	writeFile(filepath.Join(filepath.Dir(p), "job", "codex.stderr.log"), []byte("old attempt"), 0600)
	a.RunModel = func(string, string, string, M, string, M) M {
		return M{"replacement_markdown": "# 实验", "summary": "简短标题"}
	}
	a.processEditorRequests("")
	r = readMap(p)
	if str(r, "state") != "ready" || number(r, "attempt", 0) != 2 || str(r, "recovered_at") == "" {
		t.Fatal("interrupted request did not recover", r)
	}
	if string(readBytes(filepath.Join(filepath.Dir(p), "job", "codex.stderr.log"))) != "old attempt" || str(a.editorDraft(st), "markdown") != str(d, "markdown") {
		t.Fatal("recovery overwrote old logs or draft")
	}
	src := filepath.Join(t.TempDir(), "report-source.zip")
	f, e := os.Create(src)
	check(e)
	z := zip.NewWriter(f)
	w, e := z.Create("report.md")
	check(e)
	_, _ = w.Write([]byte("# Editable report"))
	w, e = z.Create("images/a.png")
	check(e)
	_, _ = w.Write([]byte("img"))
	check(z.Close())
	check(f.Close())
	dest := filepath.Join(t.TempDir(), "source")
	extractEditorSource(src, dest)
	if string(readBytes(filepath.Join(dest, "report.md"))) != "# Editable report" || !exists(filepath.Join(dest, "images", "a.png")) {
		t.Fatal("source not extracted")
	}
}

func TestEditorCancellationRequeuesWithoutNetworkFailure(t *testing.T) {
	a, st := editorFixture(t)
	ctx, cancel := context.WithCancel(a.Ctx)
	a.Ctx = ctx
	defer cancel()
	r := a.newEditorRequest(st, M{"id": strings.Repeat("f", 32), "version": 1, "start": 0, "end": 4, "instruction": "缩短标题"}, "rewrite")
	p := filepath.Join(a.editorDir(st), "requests", str(r, "id"), "request.json")
	calls := 0
	jobs := []string{}
	a.RunModel = func(tool, job, prompt string, schema M, role string, plan M) M {
		calls++
		jobs = append(jobs, job)
		if !exists(filepath.Join(job, "current-artifacts", filepath.Base(str(objects(st["artifacts"])[0], "path")))) {
			t.Fatal("missing deliverable context")
		}
		if calls == 1 {
			cancel()
			panic(context.Canceled)
		}
		return M{"replacement_markdown": "# 实验", "summary": "缩短标题"}
	}
	a.processEditorRequests("")
	if r = readMap(p); str(r, "state") != "queued" || str(r, "error") != "" || number(r, "failures", 0) != 0 {
		t.Fatal("cancelled request was marked failed", r)
	}
	if str(readMap(a.data(str(obj(r, "plan"), "writer")+"-execution.json")), "state") != "interrupted" {
		t.Fatal("cancellation marked CLI unhealthy")
	}
	a.Ctx = context.Background()
	a.processEditorRequests("")
	if str(readMap(p), "state") != "ready" || calls != 2 || jobs[0] == jobs[1] || !exists(filepath.Join(jobs[0], "request-result.json")) {
		t.Fatal("retry did not preserve separate attempts")
	}
}

func TestEditorTransientRetryLimitAndManualRecovery(t *testing.T) {
	a, st := editorFixture(t)
	id := strings.Repeat("9", 32)
	r := a.newEditorRequest(st, M{"id": id, "version": 1, "start": 0, "end": 4, "instruction": "缩短标题"}, "rewrite")
	p := filepath.Join(a.editorDir(st), "requests", id, "request.json")
	calls := 0
	a.RunModel = func(tool, job, prompt string, schema M, role string, plan M) M {
		calls++
		if calls <= 3 {
			panic(diagnosticError(tool, "tls handshake eof", "log"))
		}
		return M{"replacement_markdown": "# 实验", "summary": "缩短标题"}
	}
	for i := 1; i <= 3; i++ {
		a.processEditorRequests("")
		r = readMap(p)
		if calls != i || strings.Contains(str(r, "error"), "15 分钟") {
			t.Fatal("wrong execution count or misleading retry notice", r)
		}
		if i < 3 {
			if str(r, "state") != "queued" || !parseTime(str(r, "retry_at")).After(time.Now()) {
				t.Fatal("transient error not scheduled", r)
			}
			a.processEditorRequests("")
			if calls != i {
				t.Fatal("retried before scheduled time")
			}
			r["retry_at"] = time.Now().Add(-time.Minute).Format(time.RFC3339)
			writeJSON(p, r)
		} else if str(r, "state") != "error" {
			t.Fatal("automatic retries did not stop", r)
		}
	}
	path := "/api/task/" + str(st, "task_id") + "/retry"
	editorResult(t, editorCall(t, a, path, M{"id": id}))
	editorResult(t, editorCall(t, a, path, M{"id": id}))
	a.processEditorRequests("")
	if calls != 4 || str(readMap(p), "state") != "ready" {
		t.Fatal("manual retry failed or duplicated")
	}
	st = readMap(a.taskPath(str(st, "task_id")))
	if str(st, "status") != "awaiting" || number(st, "revision", 0) != 1 || str(st, "card_message_id") != "card1" {
		t.Fatal("retry changed published task", st)
	}
}

func TestEditorAuthenticationErrorAndCLIRetry(t *testing.T) {
	a, st := editorFixture(t)
	id := strings.Repeat("8", 32)
	a.newEditorRequest(st, M{"id": id, "version": 1, "start": 0, "end": 4, "instruction": "缩短标题"}, "rewrite")
	p := filepath.Join(a.editorDir(st), "requests", id, "request.json")
	calls := 0
	a.RunModel = func(tool, job, prompt string, schema M, role string, plan M) M {
		calls++
		panic(diagnosticError(tool, "401 unauthorized", "log"))
	}
	a.processEditorRequests("")
	a.processEditorRequests("")
	r := readMap(p)
	if str(r, "state") != "error" || calls != 1 || !strings.Contains(str(r, "error"), "完成登录") {
		t.Fatal("authentication failure was not actionable", r)
	}
	if a.retryTool(str(obj(r, "plan"), "writer")) != 1 || calls != 1 || str(readMap(p), "state") != "queued" {
		t.Fatal("CLI retry did not queue failed edit")
	}
}

func TestEditorReloadKeepsVersionMonotonicAndBacksUpDraft(t *testing.T) {
	a, st := editorFixture(t)
	d := a.editorDraft(st)
	a.saveEditorDraft(st, M{"version": d["version"], "markdown": "later edits"})
	path := "/api/task/" + str(st, "task_id")
	result := editorResult(t, editorCall(t, a, path+"/reload", M{"version": 2}))
	if number(obj(result, "draft"), "version", 0) != 3 {
		t.Fatal("version reset during reload")
	}
	files, _ := filepath.Glob(filepath.Join(a.editorDir(st), "backups", "*.json"))
	if len(files) != 1 || str(readMap(files[0]), "markdown") != "later edits" {
		t.Fatal("lost backup")
	}
	st["status"] = "working"
	st["revision"] = 2
	a.saveTask(st)
	if res := editorCall(t, a, path+"/reload", M{"version": 3}); res.Code != 409 {
		t.Fatal("loaded unfinished revision")
	}
	source := a.seedEditor(st)
	if number(source, "base_revision", 0) != 1 {
		t.Fatal("mislabelled previous published artifact")
	}
	res := editorCall(t, a, path+"/source.zip", nil)
	if res.Code != 200 {
		t.Fatal(res.Body.String())
	}
	z, e := zip.NewReader(bytes.NewReader(res.Body.Bytes()), int64(res.Body.Len()))
	check(e)
	if len(z.File) != 2 {
		t.Fatal("source ZIP must include report and screenshot")
	}
}

func TestEditorLivesWithDaemonWithoutFeishu(t *testing.T) {
	a := testApp(t)
	ctx, cancel := context.WithCancel(a.Ctx)
	a.Ctx = ctx
	defer cancel()
	writeJSON(a.data("schedule.json"), M{"last_sync_at": stamp()})
	done := make(chan struct{})
	go func() { defer close(done); a.daemon() }()
	deadline := time.Now().Add(5 * time.Second)
	var state M
	for time.Now().Before(deadline) {
		state = readMap(a.data("editor-server.json"))
		if str(state, "state") == "running" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if str(state, "state") != "running" {
		t.Fatal("editor did not start without Feishu")
	}
	u, e := url.Parse(str(state, "url"))
	check(e)
	response, e := http.Get("http://" + u.Host + "/")
	check(e)
	body, e := io.ReadAll(response.Body)
	response.Body.Close()
	check(e)
	if response.StatusCode != 200 || !bytes.Contains(body, []byte("提交成品")) {
		t.Fatal("editor page unavailable")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("daemon failed to stop")
	}
	if str(readMap(a.data("editor-server.json")), "state") != "stopped" {
		t.Fatal("editor not closed with daemon")
	}
}
