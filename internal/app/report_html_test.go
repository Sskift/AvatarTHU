package app

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func reportPNG() []byte {
	b, e := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aR4sAAAAASUVORK5CYII=")
	check(e)
	return b
}
func TestStandaloneReportIncludesImagesAndContents(t *testing.T) {
	source := t.TempDir()
	writeFile(filepath.Join(source, "screenshots", "结果.png"), reportPNG(), 0600)
	result := string(standaloneReportHTML("# 实验报告\n\n## 怎么使用\n\n点击运行。\n\n## 运行结果\n\n![截图](screenshots/结果.png)\n\n<script>alert(1)</script>", source, "报告"))
	for _, text := range []string{"<!doctype html>", "data:image/png;base64,", "报告目录", `href="#section-1"`, `id="section-2"`, "点击运行。", "@media print"} {
		if !strings.Contains(result, text) {
			t.Fatalf("missing %s", text)
		}
	}
	for _, text := range []string{"<script>", "127.0.0.1", "token=", source, `src="screenshots/`} {
		if strings.Contains(result, text) {
			t.Fatalf("unexpected %s", text)
		}
	}
	expectError(t, "相对路径图片", func() { standaloneReportHTML("![图](https://example.com/a.png)", source, "报告") })
}
func TestReportHTMLGeneratedBeforeIndependentReview(t *testing.T) {
	a := testApp(t)
	st := fixture(t, a)
	job := filepath.Join(str(st, "assignment_dir"), "writer")
	mkdir(job)
	a.RunModel = func(engine, dir, prompt string, schema M, role string, plan M) M {
		if role == "writer" {
			markdown := "# 报告\n\n## 样例\n\n![截图](screenshots/result.png)"
			writeFile(filepath.Join(dir, "final", "report.md"), []byte(markdown), 0600)
			writeFile(filepath.Join(dir, "review.md"), []byte("主写记录"), 0600)
			var buf bytes.Buffer
			z := zip.NewWriter(&buf)
			entry, e := z.Create("report.md")
			check(e)
			_, e = entry.Write([]byte(markdown))
			check(e)
			entry, e = z.Create("screenshots/result.png")
			check(e)
			_, e = entry.Write(reportPNG())
			check(e)
			check(z.Close())
			writeFile(filepath.Join(dir, "final", "report-source.zip"), buf.Bytes(), 0600)
			return M{"ready": true, "summary": "完成", "files": []string{"final/report.md", "final/report-source.zip"}, "blockers": []string{}}
		}
		body, e := os.ReadFile(filepath.Join(dir, "candidate", "report.html"))
		check(e)
		if !bytes.Contains(body, []byte("data:image/png;base64,")) {
			t.Fatal("reviewer did not receive standalone HTML")
		}
		return M{"approved": true, "summary": "通过", "comments": []M{}, "checks": []string{"阅读报告"}, "limitations": []string{}}
	}
	a.prepare(st, job)
	plan := a.pairing()
	result := a.writer(job, "生成报告", plan)
	if !containsText(texts(result["files"]), "final/report.html") {
		t.Fatal("HTML absent from artifacts")
	}
	a.review(st, result, job, plan)
	cached := a.writer(job, "cache", plan)
	if !containsText(texts(cached["files"]), "final/report.html") {
		t.Fatal("HTML lost from completed result")
	}
}
func TestReportHTMLExportLeavesFrozenArtifactsUntouched(t *testing.T) {
	a, st := editorFixture(t)
	d := a.editorDraft(st)
	writeFile(filepath.Join(str(d, "source_dir"), "screenshots", "example.png"), reportPNG(), 0600)
	before := digest(str(st, "submission"))
	response := editorCall(t, a, "/api/task/"+str(st, "task_id")+"/report.html?download=1", nil)
	if response.Code != 200 || !strings.Contains(response.Header().Get("Content-Disposition"), "report.html") {
		t.Fatal(response.Code, response.Body.String())
	}
	if !exists(filepath.Join(a.editorDir(st), "exports", "report.html")) || digest(str(st, "submission")) != before {
		t.Fatal("export changed submission or was not saved")
	}
}
