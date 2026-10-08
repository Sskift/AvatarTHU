package app

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWorkspaceCatalogAndReviewHistory(t *testing.T) {
	a, st := editorFixture(t)
	course := filepath.Dir(str(st, "courseware"))
	writeJSON(filepath.Join(course, "course.json"), M{"kcm": "课程", "xnxq": "2026", "wlkcid": "one", "student_id": "PRIVATE_STUDENT"})
	writeJSON(a.data("courses", "2025", "旧课程", "course.json"), M{"kcm": "旧课程", "xnxq": "2025"})
	writeJSON(filepath.Join(a.Root, "courses", "2026", "没有作业的课程", "course.json"), M{"kcm": "没有作业的课程", "xnxq": "2026"})
	st["review_history"] = []M{{"revision": 1, "round": 1, "approved": false, "comments": []string{"补充截图"}, "reviewer": "claude", "writer_job": "PRIVATE_WRITER_CHAT"}, {"revision": 1, "round": 2, "approved": true, "summary": "通过", "reviewer": "codex"}}
	a.saveTask(st)
	before := string(readBytes(a.taskPath(str(st, "task_id"))))
	result := editorResult(t, editorCall(t, a, "/api/workspace", nil))
	if len(objects(result["courses"])) != 3 || len(objects(result["tasks"])) != 1 || strings.Contains(string(jsonBytes(result)), "PRIVATE_STUDENT") {
		t.Fatal(result)
	}
	task := editorResult(t, editorCall(t, a, "/api/workspace/task/"+str(st, "task_id"), nil))
	if len(objects(task["reviews"])) != 2 || len(objects(task["revisions"])) != 1 || strings.Contains(string(jsonBytes(task)), "PRIVATE_WRITER_CHAT") {
		t.Fatal(task)
	}
	if string(readBytes(a.taskPath(str(st, "task_id")))) != before {
		t.Fatal("browsing changed task receipts")
	}
}
func TestWorkspaceFileVisibilityAndAuthentication(t *testing.T) {
	a, st := editorFixture(t)
	c := a.workspaceCourses()[0]
	for name, body := range map[string]string{"notices/a.md": "公告", "notices/a.json": "PRIVATE_RECEIPT", "courseware/index.json": "PRIVATE_INDEX", "courseware/.login.json": "PRIVATE_LOGIN", "homework/题目/runs/writer/chat.txt": "PRIVATE_CHAT", "courseware/user/demo.html": "<script>fetch('/api/workspace')</script>", "courseware/user/empty.txt": ""} {
		writeFile(filepath.Join(c.Dir, filepath.FromSlash(name)), []byte(body), 0600)
	}
	res := editorCall(t, a, "/api/workspace/files", nil)
	if res.Code != 200 {
		t.Fatal(res.Body.String())
	}
	for _, secret := range []string{"a.json", "index.json", ".login.json", "chat.txt"} {
		if strings.Contains(res.Body.String(), secret) {
			t.Fatal("private file in browser listing", secret)
		}
	}
	for _, path := range []string{"courseware/../course.json", "courseware\\..\\config.json", "homework/题目/runs/writer/chat.txt", "notices/a.json"} {
		r := editorCall(t, a, "/api/workspace/file?"+url.Values{"course": {c.ID}, "path": {path}}.Encode(), nil)
		if r.Code == 200 {
			t.Fatal("private path exposed", path)
		}
	}
	r := editorCall(t, a, "/api/workspace/file?"+url.Values{"course": {c.ID}, "path": {"courseware/user/demo.html"}, "inline": {"1"}}.Encode(), nil)
	if r.Code != 200 || !strings.Contains(r.Header().Get("Content-Security-Policy"), "sandbox") || !strings.Contains(r.Header().Get("Content-Disposition"), "inline") {
		t.Fatal(r.Code, r.Header())
	}
	r = editorCall(t, a, "/api/workspace/preview?"+url.Values{"course": {c.ID}, "path": {"courseware/user/empty.txt"}}.Encode(), nil)
	if r.Code != 200 {
		t.Fatal("empty text file is inaccessible", r.Body.String())
	}
	for _, path := range []string{"/api/workspace", "/api/workspace/files", "/api/workspace/file?course=" + c.ID + "&path=courseware/chapter.txt"} {
		request := httptest.NewRequest("GET", "http://127.0.0.1:7777"+path, nil)
		out := httptest.NewRecorder()
		a.editorHandler("secret", "127.0.0.1:7777").ServeHTTP(out, request)
		if out.Code != 401 {
			t.Fatal("unauthenticated course access", out.Code)
		}
	}
	req := httptest.NewRequest("POST", "http://127.0.0.1:7777/api/workspace/scan", strings.NewReader("{}"))
	req.AddCookie(&http.Cookie{Name: editorCookieName("127.0.0.1:7777"), Value: "secret"})
	req.Header.Set("Origin", "https://untrusted.example")
	out := httptest.NewRecorder()
	a.editorHandler("secret", "127.0.0.1:7777").ServeHTTP(out, req)
	if out.Code != 403 {
		t.Fatal("cross-origin mutation allowed")
	}
	if digest(str(st, "submission")) != str(st, "sha256") {
		t.Fatal("submission changed")
	}
}

func TestWorkspaceListsDownloadsOnceAndStillServesSubmissionContents(t *testing.T) {
	a, st := editorFixture(t)
	c := a.workspaceCourses()[0]
	artifact := str(objects(st["artifacts"])[0], "path")
	copyPath := filepath.Join(a.outputDir(st), "submission", filepath.Base(artifact))
	copyRelative := filepath.ToSlash(relative(c.Dir, copyPath))
	listed := false
	for _, file := range a.workspaceFiles(c) {
		if str(file, "path") == copyRelative {
			t.Fatal("submission contents duplicated in download list")
		}
		listed = listed || str(file, "path") == filepath.ToSlash(relative(c.Dir, artifact))
	}
	if !listed {
		t.Fatal("original download is missing")
	}
	r := editorCall(t, a, "/api/workspace/file?"+url.Values{"course": {c.ID}, "path": {copyRelative}}.Encode(), nil)
	if r.Code != 200 || !bytes.Equal(r.Body.Bytes(), readBytes(artifact)) {
		t.Fatal("submission content is inaccessible", r.Code, r.Body.String())
	}
}
func uploadWorkspace(t *testing.T, a *App, c workspaceCourse, name, body string) *httptest.ResponseRecorder {
	t.Helper()
	var buffer bytes.Buffer
	form := multipart.NewWriter(&buffer)
	f, e := form.CreateFormFile("file", name)
	check(e)
	_, e = f.Write([]byte(body))
	check(e)
	check(form.Close())
	req := httptest.NewRequest("POST", "http://127.0.0.1:7777/api/workspace/upload?course="+c.ID, &buffer)
	req.Header.Set("Content-Type", form.FormDataContentType())
	req.AddCookie(&http.Cookie{Name: editorCookieName("127.0.0.1:7777"), Value: "secret"})
	out := httptest.NewRecorder()
	a.editorHandler("secret", "127.0.0.1:7777").ServeHTTP(out, req)
	return out
}
func TestWorkspaceUploadRenameAndRecoverableRemoval(t *testing.T) {
	a, st := editorFixture(t)
	c := a.workspaceCourses()[0]
	before := digest(str(st, "submission"))
	editorResult(t, uploadWorkspace(t, a, c, "supplement.txt", "用户补充"))
	if r := uploadWorkspace(t, a, c, "supplement.txt", "overwrite"); r.Code != 409 {
		t.Fatal("overwrote supplemental file")
	}
	editorResult(t, editorCall(t, a, "/api/workspace/file-action", M{"course": c.ID, "path": "courseware/user/supplement.txt", "action": "rename", "name": "renamed.txt"}))
	if string(readBytes(filepath.Join(c.Dir, "courseware", "user", "renamed.txt"))) != "用户补充" {
		t.Fatal("rename changed contents")
	}
	for _, rel := range []string{"courseware/chapter.txt", filepath.ToSlash(relative(c.Dir, str(objects(st["artifacts"])[0], "path")))} {
		if r := editorCall(t, a, "/api/workspace/file-action", M{"course": c.ID, "path": rel, "action": "remove"}); r.Code != 409 {
			t.Fatal("removed managed/frozen file")
		}
	}
	editorResult(t, editorCall(t, a, "/api/workspace/file-action", M{"course": c.ID, "path": "courseware/user/renamed.txt", "action": "remove"}))
	backups, _ := filepath.Glob(a.data("workspace-trash", "*", "renamed.txt"))
	if len(backups) != 1 || string(readBytes(backups[0])) != "用户补充" {
		t.Fatal("deleted without recovery")
	}
	if exists(filepath.Join(c.Dir, "courseware", "user", "renamed.txt")) || digest(str(st, "submission")) != before {
		t.Fatal("incorrect removal or mutated frozen artifact")
	}
}
func TestWorkspaceRejectsSymlinkUpload(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows symlinks require Developer Mode")
	}
	a, _ := editorFixture(t)
	c := a.workspaceCourses()[0]
	outside := t.TempDir()
	check(os.Symlink(outside, filepath.Join(c.Dir, "courseware", "user")))
	if r := uploadWorkspace(t, a, c, "test.txt", "should stay inside"); r.Code != 409 {
		t.Fatal("uploaded via a symlink", r.Code)
	}
	if exists(filepath.Join(outside, "test.txt")) {
		t.Fatal("wrote outside course")
	}
}
func TestWorkspaceSettingsAndManualScan(t *testing.T) {
	a, _ := editorFixture(t)
	cfg := a.config()
	cfg["max_review_rounds"] = 7
	cfg["preserved_setting"] = "keep"
	a.saveConfig(cfg)
	for _, pair := range [][2]string{{"claude", "claude"}, {"codex", "codex"}, {"codex", "claude"}, {"claude", "codex"}} {
		result := editorResult(t, editorCall(t, a, "/api/workspace/settings", M{"writer": pair[0], "reviewer": pair[1], "poll_hours": 0.5}))
		if str(result, "writer") != pair[0] || str(result, "reviewer") != pair[1] || number(a.config(), "poll_interval_seconds", 0) != 1800 {
			t.Fatal(result)
		}
	}
	if str(a.config(), "preserved_setting") != "keep" || number(a.config(), "max_review_rounds", 0) != 7 {
		t.Fatal("settings lost")
	}
	if r := editorCall(t, a, "/api/workspace/settings", M{"writer": "bad", "reviewer": "codex", "poll_hours": 12}); r.Code != 409 {
		t.Fatal("invalid harness accepted")
	}
	first := editorResult(t, editorCall(t, a, "/api/workspace/scan", M{}))
	second := editorResult(t, editorCall(t, a, "/api/workspace/scan", M{}))
	if str(first, "state") != "queued" || str(first, "id") != str(second, "id") {
		t.Fatal("duplicate scan queued")
	}
	var stored M
	check(json.Unmarshal(readBytes(a.data("workspace-scan.json")), &stored))
	if str(stored, "id") != str(first, "id") {
		t.Fatal("scan not durable")
	}
}

func TestWorkspaceManualScanRunsBeforeScheduledTime(t *testing.T) {
	a := testApp(t)
	writeJSON(a.data("schedule.json"), M{"last_sync_at": stamp()})
	writeJSON(a.data("workspace-scan.json"), M{"id": "manual", "state": "queued"})
	if a.run(true, true, "") {
		t.Fatal("missing school login should fail")
	}
	result := readMap(a.data("workspace-scan.json"))
	if str(result, "state") != "error" || str(result, "error") == "" || str(result, "completed_at") == "" {
		t.Fatal("manual request did not run before the next scheduled scan", result)
	}
}

func TestWorkspaceHealthDoesNotReuseStaleSuccess(t *testing.T) {
	a := testApp(t)
	writeJSON(filepath.Join(filepath.Dir(a.session()), "keepalive-status.json"), M{"state": "valid", "last_success": "2020-01-01T00:00:00Z"})
	writeJSON(a.data("cli-health.json"), []M{{"tool": "claude", "usable": true, "checked_at": "2020-01-01T00:00:00Z"}, {"tool": "codex", "usable": true, "checked_at": stamp()}})
	writeJSON(a.data("codex-execution.json"), M{"state": "failed", "reason": "额度不足", "action": "稍后重试"})
	health := a.workspaceHealth()
	if str(obj(health, "learn"), "state") != "stale" {
		t.Fatal("stale Learn status marked valid")
	}
	for _, c := range objects(health["cli"]) {
		if str(c, "tool") == "claude" && str(c, "state") != "stale" {
			t.Fatal(c)
		}
		if str(c, "tool") == "codex" && str(c, "state") != "failed" {
			t.Fatal("CLI login check masked actual failure", c)
		}
	}
}

func TestWorkspaceArtifactsThroughFriendlyHomeAlias(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows symlinks require Developer Mode")
	}
	a := testApp(t)
	actual := a.Root
	alias := filepath.Join(t.TempDir(), "friendly")
	check(os.Symlink(actual, alias))
	a.Root = alias
	st := frozen(t, a)
	result := editorResult(t, editorCall(t, a, "/api/workspace/task/"+str(st, "task_id"), nil))
	if len(objects(result["revisions"])) != 1 {
		t.Fatal("artifacts not found through home symlink", result)
	}
}
