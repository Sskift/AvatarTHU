package app

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
)

func testApp(t *testing.T) *App {
	t.Helper()
	a := New(context.Background(), t.TempDir())
	a.saveConfig(M{"lark_enabled": false, "max_review_rounds": 3})
	return a
}
func fixture(t *testing.T, a *App) M {
	t.Helper()
	assignment := filepath.Join(a.Root, "courses", "2026", "课程", "homework", "题目")
	source := filepath.Join(assignment, "source")
	ware := filepath.Join(a.Root, "courses", "2026", "课程", "courseware")
	writeFile(filepath.Join(source, "question.txt"), []byte("1+1?"), 0600)
	writeFile(filepath.Join(ware, "chapter.txt"), []byte("加法"), 0600)
	return M{"task_id": "0123456789abcdef", "revision": 1, "status": "queued", "title": "题目 <script>bad</script>", "course": "课程", "deadline": "2099-01-01 12:00:00", "description": "# 原题\n\n1. 求 1+1\n2. 给出理由\n\n$x_1+x_2=2$", "folder": source, "assignment_dir": assignment, "courseware": ware}
}
func resultAt(job, answer string) M {
	writeFile(filepath.Join(job, "final", "答案.txt"), []byte(answer), 0600)
	writeFile(filepath.Join(job, "review.md"), []byte("PRIVATE SELF ASSESSMENT"), 0600)
	return M{"ready": true, "summary": "1. 已计算\n2. 已自查", "files": []string{"final/答案.txt"}, "blockers": []string{}, "presentation": M{"assignment": "求和", "checks": []string{"已计算"}, "highlights": []M{}}}
}
func frozen(t *testing.T, a *App) M {
	st := fixture(t, a)
	job := filepath.Join(str(st, "assignment_dir"), "runs", "r1", "writer")
	r := resultAt(job, "2")
	st["job"] = job
	a.snapshot(st, r, job)
	merge(st, M{"status": "awaiting", "card_message_id": "card1", "chat_id": "chat"})
	a.saveTask(st)
	return st
}
func expectError(t *testing.T, contains string, f func()) {
	t.Helper()
	err := attempt(f)
	if err == nil || !strings.Contains(err.Error(), contains) {
		t.Fatalf("expected error %q, got %v", contains, err)
	}
}
func TestLegacyFingerprint(t *testing.T) {
	v := M{"中文": "<a>\u2028", "list": []any{"x", nil, true, 12}}
	want := `{"list": ["x", null, true, 12], "中文": "<a>` + "\u2028" + `"}`
	if got := canonical(v); got != want {
		t.Fatalf("%q != %q", got, want)
	}
	p := filepath.Join(t.TempDir(), "index.json")
	old := []byte("[\n  {\n    \"path\": \"x\",\n    \"title\": \"中文\"\n  }\n]\n")
	writeFile(p, old, 0600)
	writeJSON(p, []M{{"title": "中文", "path": "x"}})
	b, e := os.ReadFile(p)
	check(e)
	if !bytes.Equal(b, old) {
		t.Fatal("format-only rewrite changed legacy material hash")
	}
}
func TestWorkflowBothModesRejectThenApprove(t *testing.T) {
	for _, mode := range []string{"claude-codex", "codex-claude"} {
		t.Run(mode, func(t *testing.T) {
			a := testApp(t)
			cfg := a.config()
			cfg["review_mode"] = mode
			a.saveConfig(cfg)
			st := fixture(t, a)
			writers, reviews := 0, 0
			folders := map[string]bool{}
			a.RunModel = func(engine, job, prompt string, schema M, role string, plan M) M {
				if folders[job] {
					t.Fatal("reused process folder")
				}
				folders[job] = true
				if role == "writer" {
					writers++
					if engine != strings.Split(mode, "-")[0] {
						t.Fatal("wrong writer")
					}
					if writers == 2 {
						if !strings.Contains(prompt, "fix answer") || !exists(filepath.Join(job, "previous-final", "答案.txt")) {
							t.Fatal("rewrite did not get feedback and previous files")
						}
					}
					return resultAt(job, fmt.Sprint(writers))
				}
				reviews++
				if engine != strings.Split(mode, "-")[1] {
					t.Fatal("wrong reviewer")
				}
				if exists(filepath.Join(job, "review.md")) || exists(filepath.Join(job, "previous-final", "答案.txt")) {
					t.Fatal("writer history reached reviewer")
				}
				for _, p := range visibleFiles(job) {
					b, e := os.ReadFile(p)
					check(e)
					if strings.Contains(string(b), "PRIVATE SELF ASSESSMENT") || strings.Contains(string(b), "fix answer") {
						t.Fatal("review context leaked")
					}
				}
				return M{"approved": reviews == 2, "summary": "independent", "comments": []M{{"location": "answer", "comment": "fix answer", "suggestion": "calculate again"}}, "checks": []string{"addition"}, "limitations": []string{}}
			}
			a.process(st)
			if writers != 2 || reviews != 2 || str(st, "status") != "awaiting" || len(objects(st["review_history"])) != 2 {
				t.Fatalf("unexpected workflow: %d %d %v", writers, reviews, st["status"])
			}
			if !strings.HasPrefix(str(st, "job"), str(st, "assignment_dir")) || !strings.HasPrefix(str(st, "output_dir"), str(st, "assignment_dir")) {
				t.Fatal("files outside homework")
			}
			page, e := os.ReadFile(str(st, "local_review"))
			check(e)
			for _, s := range []string{"一、作业描述", "五、历次独立复审", "fix answer", "Content-Security-Policy", "&lt;script&gt;"} {
				if !strings.Contains(string(page), s) {
					t.Fatalf("missing %s", s)
				}
			}
			if strings.Contains(string(page), "<script>") {
				t.Fatal("unescaped local content")
			}
			a.process(st)
			if writers != 2 {
				t.Fatal("reran completed assignment")
			}
		})
	}
}
func TestReviewLimitAndBlockedWriter(t *testing.T) {
	a := testApp(t)
	a.saveConfig(M{"lark_enabled": false, "max_review_rounds": 1})
	st := fixture(t, a)
	a.RunModel = func(_, job, _ string, _ M, role string, _ M) M {
		if role == "writer" {
			return resultAt(job, "wrong")
		}
		return M{"approved": false, "summary": "wrong", "comments": []M{}, "checks": []string{}, "limitations": []string{"important unknown"}}
	}
	a.process(st)
	if str(st, "status") != "needs_student" || boolean(st, "ready") || len(texts(st["blockers"])) == 0 {
		t.Fatal(st)
	}
	if len(objects(st["review_history"])) != 1 {
		t.Fatal("lost review")
	}
}
func TestWriterAndReviewCheckpoints(t *testing.T) {
	a := testApp(t)
	st := fixture(t, a)
	job := filepath.Join(str(st, "assignment_dir"), "runs", "writer")
	a.prepare(st, job)
	r := resultAt(job, "2")
	writeJSON(filepath.Join(job, "result.json"), r)
	writeJSON(filepath.Join(job, "complete.json"), M{"hashes": hashesFor(job, []string{"final/答案.txt", "review.md"})})
	a.RunModel = func(string, string, string, M, string, M) M { t.Fatal("unnecessary model call"); return nil }
	a.writer(job, "", a.pairing())
	hash := fingerprint(hashesFor(job, texts(r["files"])))
	reviewJob := filepath.Join(filepath.Dir(job), "review")
	cp := M{"job": reviewJob, "writer_job": job, "candidate_hash": hash}
	st["review_attempt"] = cp
	receipt := M{"id": "review", "approved": true, "summary": "done", "comments": []M{}, "checks": []string{}, "limitations": []string{}, "writer_job": job, "candidate_hash": hash}
	writeJSON(filepath.Join(reviewJob, "complete.json"), receipt)
	a.review(st, r, job, a.pairing())
	a.review(st, r, job, a.pairing())
	if len(objects(st["review_history"])) != 1 {
		t.Fatal("duplicate or missing review")
	}
	writeFile(filepath.Join(job, "final", "答案.txt"), []byte("changed"), 0600)
	expectError(t, "变化", func() { a.writer(job, "", a.pairing()) })
}
func TestReviewCandidateMutationRejected(t *testing.T) {
	a := testApp(t)
	st := fixture(t, a)
	job := filepath.Join(str(st, "assignment_dir"), "runs", "writer")
	a.prepare(st, job)
	r := resultAt(job, "2")
	a.RunModel = func(_, job, _ string, _ M, _ string, _ M) M {
		writeFile(filepath.Join(job, "candidate", "答案.txt"), []byte("3"), 0600)
		return M{"approved": true, "summary": "ok", "comments": []M{}, "checks": []string{}, "limitations": []string{}}
	}
	expectError(t, "修改了候选", func() { a.review(st, r, job, a.pairing()) })
	if len(objects(st["review_history"])) != 0 {
		t.Fatal("accepted changed candidate")
	}
}
func TestNoticeReceiptBeforeReadAndRetry(t *testing.T) {
	a := testApp(t)
	a.saveConfig(M{"lark_enabled": true})
	sent := 0
	a.SendCard = func(M, string) M { sent++; return M{"message_id": "sent"} }
	note := M{"unread": true, "course_id": "cid", "id": "nid", "title": "notice", "body": "body"}
	markCalls := 0
	mark := func(M) {
		markCalls++
		if str(obj(obj(readMap(a.data("notices.json")), "cid:nid"), "message"), "message_id") != "sent" {
			t.Fatal("read before receipt")
		}
		if markCalls == 1 {
			panic(fmt.Errorf("mark failed"))
		}
	}
	expectError(t, "mark failed", func() { a.sendNotices([]M{note}, mark) })
	a.sendNotices([]M{note}, mark)
	if sent != 1 || markCalls != 2 {
		t.Fatal(sent, markCalls)
	}
	a.saveConfig(M{"lark_enabled": false})
	a.sendNotices([]M{note}, func(M) { t.Fatal("marked read while Feishu disabled") })
}
func ownerEvent(st M) M {
	return M{"operator_id": "owner", "event_id": "event1", "message_id": "card1", "action_value": M{"action": "submit", "task_id": st["task_id"], "revision": st["revision"], "nonce": st["nonce"], "sha256": st["sha256"]}}
}
func TestCardApprovalAndUnknownNeverRetry(t *testing.T) {
	for _, scenario := range []string{"success", "unknown", "tamper", "wrong-owner", "stale-card", "stale-version", "stale-nonce", "stale-hash"} {
		t.Run(scenario, func(t *testing.T) {
			a := testApp(t)
			a.saveConfig(M{"lark_enabled": true, "lark_user_id": "owner"})
			st := frozen(t, a)
			event := ownerEvent(st)
			switch scenario {
			case "tamper":
				writeFile(str(st, "submission"), []byte("tampered"), 0600)
			case "wrong-owner":
				event["operator_id"] = "stranger"
			case "stale-card":
				event["message_id"] = "old"
			case "stale-version":
				obj(event, "action_value")["revision"] = 99
			case "stale-nonce":
				obj(event, "action_value")["nonce"] = "old"
			case "stale-hash":
				obj(event, "action_value")["sha256"] = "old"
			}
			uploads := 0
			a.SendCard = func(M, string) M { return M{"message_id": "receipt"} }
			a.Verify = func(M) *School { return nil }
			a.Upload = func(_ *School, st M, _ string) M {
				uploads++
				if str(readMap(a.taskPath(str(st, "task_id"))), "status") != "submitting" {
					t.Fatal("upload before claim")
				}
				if scenario == "unknown" {
					panic(fmt.Errorf("timeout"))
				}
				return M{"result": "success"}
			}
			a.act(event)
			a.act(event)
			actual := readMap(a.taskPath(str(st, "task_id")))
			switch scenario {
			case "success":
				if uploads != 1 || str(actual, "status") != "submitted" {
					t.Fatal(uploads, actual)
				}
			case "unknown":
				if uploads != 1 || str(actual, "status") != "submission_unknown" {
					t.Fatal(uploads, actual)
				}
			case "tamper":
				if uploads != 0 || str(actual, "status") != "approval_invalid" {
					t.Fatal(uploads, actual)
				}
			default:
				if uploads != 0 {
					t.Fatal("invalid approval uploaded")
				}
			}
		})
	}
}
func TestOwnerFeedbackInvalidatesOldCard(t *testing.T) {
	a := testApp(t)
	a.saveConfig(M{"lark_enabled": true, "lark_user_id": "owner"})
	st := frozen(t, a)
	a.SendCard = func(M, string) M { return M{"message_id": "receipt"} }
	event := ownerEvent(st)
	event["action_name"] = "revise"
	event["form_value"] = `{"feedback":"补充步骤"}`
	if !a.act(event) {
		t.Fatal("form not handled")
	}
	if str(readMap(a.taskPath(str(st, "task_id"))), "status") != "revision_ready" {
		t.Fatal("old card still active")
	}
	if a.act(ownerEvent(st)) {
		t.Fatal("old submit handled after feedback")
	}
}
func mockCloud(t *testing.T, a *App, ambiguous bool) *int {
	t.Helper()
	creates := new(int)
	a.CallLark = func(args []string) M {
		joined := strings.Join(args, " ")
		if strings.Contains(joined, "init-draft") {
			folder := filepath.Join(a.Root, "draft-workspace")
			mkdir(folder)
			return M{"draft_path": filepath.Join(folder, "draft.xml")}
		}
		if strings.Contains(joined, "--command parse") {
			return M{"assessment": M{"status": "passed"}}
		}
		if strings.Contains(joined, "+create") {
			*creates++
			if ambiguous {
				panic(fmt.Errorf("create timed out"))
			}
			return M{"document": M{"document_id": "doc1", "url": "https://example.test/doc1"}, "warnings": []any{}}
		}
		if strings.Contains(joined, "+fetch") {
			var st M
			for _, s := range a.tasks() {
				st = s
			}
			b, e := os.ReadFile(str(obj(st, "review_doc"), "draft"))
			check(e)
			content := regexp.MustCompile(`<source path="[^"]*"`).ReplaceAllString(string(b), `<source token="filetoken"`)
			return M{"document": M{"content": content}}
		}
		t.Fatal(joined)
		return nil
	}
	return creates
}
func TestCloudOwnsAllFilesAndCreateReceipt(t *testing.T) {
	a := testApp(t)
	a.saveConfig(M{"lark_enabled": true, "lark_user_id": "owner"})
	st := frozen(t, a)
	creates := mockCloud(t, a, false)
	messages := 0
	a.SendCard = func(card M, _ string) M {
		messages++
		if str(card, "schema") != "2.0" {
			t.Fatal("not a card")
		}
		return M{"message_id": "new-card"}
	}
	a.deliver(st)
	a.deliver(st)
	if *creates != 1 || messages != 1 || !boolean(obj(st, "review_doc"), "verified") {
		t.Fatal(*creates, messages, st)
	}
	if len(texts(obj(st, "review_doc")["attachment_names"])) < 2 {
		t.Fatal("missing attachments")
	}
}
func TestUnknownDocumentCreateDoesNotDuplicate(t *testing.T) {
	a := testApp(t)
	a.saveConfig(M{"lark_enabled": true})
	st := frozen(t, a)
	creates := mockCloud(t, a, true)
	expectError(t, "timed out", func() { a.publishCloud(st) })
	expectError(t, "待核对", func() { a.publishCloud(st) })
	if *creates != 1 {
		t.Fatal("duplicate doc")
	}
}
func TestDocumentNativeListsMathAndEscape(t *testing.T) {
	got := prose("# Heading\n\n1. **first**\n2. next\n\n$x_1 + x_2$\n\n```txt\n$literal$\n```\n\n<script>bad</script>\n\n| a | b |\n|---|---|\n|1|2|\n")
	for _, expected := range []string{"<h2>", "<ol>", "<b>first</b>", "<latex>x_1 + x_2</latex>", "$literal$", "<table>"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("missing %s: %s", expected, got)
		}
	}
	if strings.Contains(got, "<script>") {
		t.Fatal("raw html")
	}
	decoder := xml.NewDecoder(strings.NewReader("<root>" + got + "</root>"))
	for {
		_, e := decoder.Token()
		if e == io.EOF {
			break
		}
		if e != nil {
			t.Fatal(e)
		}
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func fakeSchool(a *App, handler roundTrip) *School {
	s := a.schoolFrom(M{"all_cookies": []M{{"name": "XSRF-TOKEN", "value": "secret", "domain": "learn.tsinghua.edu.cn", "path": "/"}}})
	s.HTTP.Transport = handler
	return s
}
func response(r *http.Request, body, ctype string, length int64) *http.Response {
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {ctype}}, Body: io.NopCloser(strings.NewReader(body)), ContentLength: length, Request: r}
}
func TestDownloadsPreserveGoodFileAndRepairCache(t *testing.T) {
	a := testApp(t)
	p := filepath.Join(a.Root, "file.pdf")
	writeFile(p, []byte("old"), 0600)
	s := fakeSchool(a, func(r *http.Request) (*http.Response, error) {
		return response(r, "partial", "application/pdf", 999), nil
	})
	expectError(t, "不完整", func() { s.download("/download", p, "v2") })
	b, e := os.ReadFile(p)
	check(e)
	if string(b) != "old" {
		t.Fatal("replaced good download")
	}
	calls := 0
	s.HTTP.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Query().Get("_csrf") != "secret" || r.Header.Get("X-XSRF-TOKEN") != "secret" {
			t.Fatal("no CSRF")
		}
		return response(r, "%PDF-x", "application/pdf", 6), nil
	})
	s.download("/download", p, "v2")
	s.download("/download", p, "v2")
	if calls != 1 {
		t.Fatal("cache missed")
	}
	writeFile(p, []byte("bad"), 0600)
	s.download("/download", p, "v2")
	if calls != 2 {
		t.Fatal("tamper not repaired")
	}
	s.HTTP.Transport = roundTrip(func(r *http.Request) (*http.Response, error) { return response(r, "login", "text/html", 5), nil })
	expectError(t, "登录页面", func() { s.download("/download", p, "v3") })
}
func TestSchoolPaginationAndCookieRotation(t *testing.T) {
	a := testApp(t)
	calls := 0
	s := fakeSchool(a, func(r *http.Request) (*http.Response, error) {
		calls++
		items := []M{}
		for i := 0; i < 100; i++ {
			items = append(items, M{"id": i})
		}
		if calls == 2 {
			items = []M{{"id": 100}}
		}
		b := string(jsonBytes(M{"result": "success", "object": M{"aaData": items}}))
		return response(r, b, "application/json", int64(len(b))), nil
	})
	if len(s.homeworkRows("course")) != 101 {
		t.Fatal("pagination incomplete")
	}
	writeJSON(a.session(), M{"all_cookies": s.Jar.snapshot()})
	s = a.school(false)
	u, _ := url.Parse(schoolBase)
	s.Jar.SetCookies(u, []*http.Cookie{{Name: "JSESSIONID", Value: "rotated", Path: "/"}})
	s.persist()
	saved := readMap(a.session())
	if len(objects(saved["all_cookies"])) != 2 {
		t.Fatal("rotation not saved")
	}
	writeJSON(a.session(), M{"all_cookies": []M{{"name": "XSRF-TOKEN", "value": "new-login", "domain": "learn.tsinghua.edu.cn"}}})
	s.Jar.SetCookies(u, []*http.Cookie{{Name: "JSESSIONID", Value: "stale", Path: "/"}})
	s.persist()
	if objects(readMap(a.session())["all_cookies"])[0]["value"] != "new-login" {
		t.Fatal("overwrote concurrent login")
	}
}
func TestBoundariesAndSettings(t *testing.T) {
	a := testApp(t)
	for _, input := range []string{"../secret", "/absolute", ".hidden", "a/../secret", "C:\\secret"} {
		expectError(t, "", func() { inside(a.Root, input) })
	}
	if duration("12h") != 43200 || duration("30m") != 1800 {
		t.Fatal("duration")
	}
	expectError(t, "1 分钟", func() { duration("1s") })
	now := time.Now()
	schedule := M{"last_sync_at": now.Format(time.RFC3339)}
	if got := a.nextScan(schedule); got.Sub(now) < 11*time.Hour {
		t.Fatal(got)
	}
	writeJSON(filepath.Join(a.Root, "keepalive-status.json"), M{"checked_at": stamp(), "state": "valid"})
	state := a.keepalive(false, false)
	if str(state, "state") != "valid" {
		t.Fatal("performed not-due keepalive")
	}
	if safeName("CON") == "CON" || strings.ContainsAny(safeName(`a:b?c/..`), `:?/`) {
		t.Fatal("Windows names")
	}
}
func TestServiceSpecifications(t *testing.T) {
	a := testApp(t)
	for _, s := range []string{a.serviceSpec(), strings.Replace(a.windowsSpec(`DOMAIN\user`), `encoding="UTF-16"`, `encoding="UTF-8"`, 1)} {
		d := xml.NewDecoder(strings.NewReader(s))
		for {
			_, e := d.Token()
			if e == io.EOF {
				break
			}
			if e != nil {
				t.Fatal(e)
			}
		}
		if strings.Contains(s, "python") {
			t.Fatal("Python service")
		}
		if !strings.Contains(s, "daemon") {
			t.Fatal("not daemon")
		}
	}
	spec := a.windowsSpec("user")
	for _, want := range []string{"InteractiveToken", "IgnoreNew", "StartWhenAvailable", "PT0S"} {
		if !strings.Contains(spec, want) {
			t.Fatal(want)
		}
	}
}
func TestDefaultModelArgumentsAndAvailabilityErrors(t *testing.T) {
	a := testApp(t)
	exe, e := os.Executable()
	check(e)
	for _, tool := range []string{"claude", "codex"} {
		args := modelCommand(tool, a.Root, writerSchema, M{tool + "_cli": exe})
		for _, arg := range args {
			if arg == "--model" || strings.HasPrefix(arg, "model=") {
				t.Fatal("overrode configured model")
			}
		}
	}
	for _, pair := range [][2]string{{"unauthorized", "登录"}, {"429 quota", "额度"}, {"unknown option", "版本"}, {"connection timeout", "网络"}, {"403 forbidden", "权限"}, {"model not found", "默认"}} {
		if !strings.Contains(diagnosticError("claude", pair[0], "log").Error(), pair[1]) {
			t.Fatal(pair)
		}
	}
}
func TestInitNeedsNoLanguageRuntime(t *testing.T) {
	a := testApp(t)
	t.Setenv("AVATARTHU_HOME", a.Root)
	a.init(true, true)
	if !exists(a.installedBinary()) {
		t.Fatal("binary not installed")
	}
	if exists(filepath.Join(a.Root, ".venv", "bin", "python")) {
		t.Fatal("created Python environment")
	}
	if number(a.config(), "poll_interval_seconds", 0) != 43200 {
		t.Fatal("default poll")
	}
}
func TestLocalMigrationPreview(t *testing.T) {
	root := os.Getenv("AVATARTHU_LEGACY_SMOKE_HOME")
	if root == "" {
		t.Skip("optional read-only check against existing local deliverables")
	}
	a := New(context.Background(), root)
	count := 0
	for _, st := range a.tasks() {
		if len(objects(st["artifacts"])) == 0 {
			continue
		}
		before := readMap(a.taskPath(str(st, "task_id")))
		a.verifiedFiles(st)
		draft := filepath.Join(root, "data", "integration-smoke", "go-migration", str(st, "task_id"), "preview.xml")
		metadata := a.buildDocument(st, draft, true)
		if len(texts(metadata["attachment_names"])) < 2 {
			t.Fatal("missing migrated attachments")
		}
		after := readMap(a.taskPath(str(st, "task_id")))
		if !reflect.DeepEqual(before, after) {
			t.Fatal("migration modified task state")
		}
		count++
	}
	t.Logf("verified frozen files and generated local document preview for %d existing tasks", count)
}

func TestRealCLIs(t *testing.T) {
	if os.Getenv("AVATARTHU_MODEL_SMOKE") != "1" {
		t.Skip("requires explicitly enabled local model smoke")
	}
	root := os.Getenv("AVATARTHU_SMOKE_DIR")
	if root == "" {
		t.Fatal("set AVATARTHU_SMOKE_DIR outside repository")
	}
	for _, mode := range []string{"claude-codex", "codex-claude"} {
		t.Run(mode, func(t *testing.T) {
			a := New(context.Background(), filepath.Join(root, mode))
			mkdir(a.Root)
			a.saveConfig(M{"lark_enabled": false, "review_mode": mode, "max_review_rounds": 1, "stage_timeout": 240})
			st := fixture(t, a)
			if os.Getenv("AVATARTHU_RESUME_SMOKE") == "1" {
				saved := readMap(a.taskPath(str(st, "task_id")))
				if len(saved) > 0 {
					st = saved
				}
			}
			st["title"] = "原生迁移算术验证"
			st["description"] = "请计算 17×23，仅交付 answer.txt，内容为一个十进制整数。无需报告、联网、PDF或额外软件。"
			if str(st, "status") == "needs_student" {
				st["status"] = "revision_ready"
				st["feedback"] = "测试原题资料已修正：只需计算 17×23 并交付 answer.txt，请依据当前 input 重新核对。"
			}
			writeFile(filepath.Join(str(st, "folder"), "question.txt"), []byte("计算 17×23，答案只写一个十进制整数。"), 0600)
			a.process(st)
			if str(st, "status") != "awaiting" || str(st, "review_outcome") != "approved" {
				t.Fatalf("live review failed: %s %s", st["status"], st["review_outcome"])
			}
			b, e := os.ReadFile(str(st, "submission"))
			check(e)
			if strings.TrimSpace(string(b)) != "391" {
				t.Fatalf("unexpected arithmetic output %q", b)
			}
			t.Logf("native %s completed writer, fresh review, frozen files and local document", mode)
		})
	}
}

func TestLiveDocumentParse(t *testing.T) {
	root := os.Getenv("AVATARTHU_LEGACY_SMOKE_HOME")
	if root == "" {
		t.Skip("optional existing-file, local Lark parser verification")
	}
	a := New(context.Background(), root)
	for _, st := range a.tasks() {
		if len(objects(st["artifacts"])) == 0 {
			continue
		}
		decision := M{"audience": "作业提交者", "reader_task": "审阅原题与本版产物", "genre_contract": nil, "adapter": nil, "presentation_mode": "rich", "visual_plan": M{"reason": "五部分原生文档", "blocks": []any{}}}
		workspace := a.lark("docs", "+script", "--command", "init-draft", "--presentation-decision", string(jsonBytes(decision)))
		draft := str(workspace, "draft_path")
		if !filepath.IsAbs(draft) {
			draft = filepath.Join(str(workspace, "cwd"), draft)
		}
		meta := a.buildDocument(st, draft, false)
		r := a.lark("docs", "+script", "--command", "parse", "--content", "@"+filepath.ToSlash(relative(a.Root, draft)))
		if str(obj(r, "assessment"), "status") != "passed" {
			t.Fatalf("parser failed: %s", jsonBytes(r))
		}
		t.Logf("native document accepted by Lark parser: %d attachments, %d images", len(texts(meta["attachment_names"])), number(meta, "image_count", 0))
	}
}

func TestWriterAcceptsUnambiguousFinalRelativeName(t *testing.T) {
	a := testApp(t)
	job := filepath.Join(a.Root, "job")
	r := resultAt(job, "2")
	r["files"] = []string{"答案.txt"}
	v := validateWriter(r, job)
	if texts(v["files"])[0] != "final/答案.txt" {
		t.Fatal(v)
	}
	r["files"] = []string{"../review.md"}
	expectError(t, "", func() { validateWriter(r, job) })
}

func TestProcessHelper(t *testing.T) {
	if os.Getenv("AVATARTHU_PROCESS_HELPER") != "1" {
		return
	}
	arg := os.Args[len(os.Args)-1]
	if arg == "wait" {
		time.Sleep(10 * time.Second)
	}
	fmt.Print(arg)
	os.Exit(0)
}
func TestProcessCaptureAndCancellation(t *testing.T) {
	a := testApp(t)
	t.Setenv("AVATARTHU_PROCESS_HELPER", "1")
	exe, e := os.Executable()
	check(e)
	arg := `中文 a&b"c\ path`
	out, _, e := capture(a.Ctx, a.Root, 10*time.Second, exe, "-test.run=^TestProcessHelper$", "--", arg)
	if e != nil || string(out) != arg {
		t.Fatalf("argument transport: %q, %v", out, e)
	}
	started := time.Now()
	_, _, e = capture(a.Ctx, a.Root, 300*time.Millisecond, exe, "-test.run=^TestProcessHelper$", "--", "wait")
	if e == nil || time.Since(started) > 5*time.Second {
		t.Fatalf("cancellation failed: %v", e)
	}
}
