package app

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func revisionZIP(path, code string) {
	var data bytes.Buffer
	w := zip.NewWriter(&data)
	f, e := w.Create("src/main.go")
	check(e)
	_, e = io.WriteString(f, code)
	check(e)
	check(w.Close())
	writeFile(path, data.Bytes(), 0600)
}

func revisionFixture(t *testing.T, a *App) M {
	st := fixture(t, a)
	st["title"] = "版本对照演示"
	first := filepath.Join(str(st, "assignment_dir"), "runs", "r1", "writer")
	r := resultAt(first, "答案：2\n验证：未补充\n")
	revisionZIP(filepath.Join(first, "final", "code.zip"), "package main\nfunc add(a, b int) int { return a - b }\n")
	r["files"] = append(texts(r["files"]), "final/code.zip")
	st["job"] = first
	a.snapshot(st, r, first)
	a.publishLocal(st)
	a.rememberRevision(st)
	st["revision"] = 2
	st["feedback"] = "补充验证，修正加法实现；补充边界说明。"
	second := filepath.Join(str(st, "assignment_dir"), "runs", "r2", "writer")
	r = resultAt(second, "答案：2\n验证：1 + 1 = 2\n")
	revisionZIP(filepath.Join(second, "final", "code.zip"), "package main\nfunc add(a, b int) int { return a + b }\n")
	r["files"] = append(texts(r["files"]), "final/code.zip")
	obj(r, "presentation")["revision_notes"] = []M{
		{"request": "补充验证，修正加法实现", "status": "addressed", "detail": "更新答案验证段和 src/main.go。", "files": []string{"答案.txt", "code.zip"}},
		{"request": "补充边界说明", "status": "partial", "detail": "大整数溢出仍需核对。", "files": []string{"code.zip"}},
	}
	st["job"] = second
	a.snapshot(st, r, second)
	a.publishLocal(st)
	return st
}

func TestRevisionDocumentUsesFrozenFiles(t *testing.T) {
	a := testApp(t)
	st := revisionFixture(t, a)
	b, e := os.ReadFile(str(st, "local_review"))
	check(e)
	text := string(b)
	for _, want := range []string{"本版改动", "【已处理】", "【部分处理】", "打开上版本地审阅页", "上版 r1 · 答案.txt", "code.zip/src/main.go", "- 验证：未补充", "+ 验证：1 + 1 = 2", "return a - b", "return a + b", "一、作业描述", "五、历次独立复审"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %s in revision document", want)
		}
	}
	if strings.Index(text, "本版改动") > strings.Index(text, "一、作业描述") {
		t.Fatal("changes must be near the beginning")
	}
	if strings.Contains(text, "PRIVATE SELF ASSESSMENT") {
		t.Fatal("compared self assessment instead of deliverables")
	}
	old := objects(st["revision_history"])[0]
	for _, item := range objects(old["artifacts"]) {
		if digest(str(item, "path")) != str(item, "sha256") {
			t.Fatal("comparison changed frozen files")
		}
	}
	draft := filepath.Join(a.reviewDir(st), "cloud-preview.xml")
	a.buildDocument(st, draft, false)
	xmlBytes, e := os.ReadFile(draft)
	check(e)
	d := xml.NewDecoder(strings.NewReader("<root>" + string(xmlBytes) + "</root>"))
	for {
		_, e := d.Token()
		if e == io.EOF {
			break
		}
		if e != nil {
			t.Fatal(e)
		}
	}
}

func TestRevisionBaselineMissingDoesNotInventChanges(t *testing.T) {
	a := testApp(t)
	st := revisionFixture(t, a)
	old := objects(st["revision_history"])[0]
	check(os.Remove(str(objects(old["artifacts"])[0], "path")))
	a.publishLocal(st)
	b, e := os.ReadFile(str(st, "local_review"))
	check(e)
	if !strings.Contains(string(b), "文件差异未生成") || strings.Contains(string(b), "新增 2 项") {
		t.Fatal("missing baseline presented as additions")
	}
	if !strings.Contains(string(b), "三、完整产物") {
		t.Fatal("lost current deliverables")
	}
}

func TestRevisionNotesKeepReviewerIndependent(t *testing.T) {
	a := testApp(t)
	st := fixture(t, a)
	st["feedback"] = "PRIVATE OWNER FEEDBACK"
	st["revision_history"] = []M{{"revision": 1, "local_review": "PRIVATE PREVIOUS REVIEW"}}
	a.RunModel = func(tool, job, prompt string, schema M, role string, plan M) M {
		if role == "writer" {
			r := resultAt(job, "2")
			obj(r, "presentation")["revision_notes"] = []M{{"request": "PRIVATE OWNER FEEDBACK", "status": "addressed", "detail": "PRIVATE WRITER CLAIM", "files": []string{"答案.txt"}}}
			return r
		}
		if strings.Contains(prompt, "PRIVATE") {
			t.Fatal("history in reviewer prompt")
		}
		for _, p := range visibleFiles(job) {
			b, e := os.ReadFile(p)
			check(e)
			if bytes.Contains(b, []byte("PRIVATE")) {
				t.Fatal("history in reviewer files")
			}
		}
		return M{"approved": true, "summary": "checked", "comments": []M{}, "checks": []string{}, "limitations": []string{}}
	}
	a.process(st)
}

// Optional local preview and Lark parse only; it never creates a cloud document,
// sends a message, calls a model, or submits homework.
func TestRevisionPreview(t *testing.T) {
	root := os.Getenv("AVATARTHU_REVISION_PREVIEW_HOME")
	if root == "" {
		t.Skip("optional revision preview")
	}
	a := New(t.Context(), root)
	st := revisionFixture(t, a)
	if cli := os.Getenv("AVATARTHU_REVISION_PARSE_CLI"); cli != "" {
		a.saveConfig(M{"lark_enabled": true, "lark_cli": cli})
		decision := M{"audience": "作业提交者", "reader_task": "对照前后版本审阅修改", "genre_contract": nil, "adapter": nil, "presentation_mode": "rich", "visual_plan": M{"reason": "版本对照和五部分审阅", "blocks": []any{}}}
		workspace := a.lark("docs", "+script", "--command", "init-draft", "--presentation-decision", string(jsonBytes(decision)))
		draft := str(workspace, "draft_path")
		if !filepath.IsAbs(draft) {
			draft = filepath.Join(str(workspace, "cwd"), draft)
		}
		a.buildDocument(st, draft, false)
		parsed := a.lark("docs", "+script", "--command", "parse", "--content", "@"+filepath.ToSlash(relative(a.Root, draft)))
		if str(obj(parsed, "assessment"), "status") != "passed" {
			t.Fatalf("Lark parser: %s", jsonBytes(parsed))
		}
	}
	t.Log(str(st, "local_review"))
}
