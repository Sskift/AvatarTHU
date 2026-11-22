package app

import (
	"archive/zip"
	"bytes"
	"io"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

func TestSubmissionReviewTracksExactPathsAndFrozenBytes(t *testing.T) {
	a, st := editorFixture(t)
	tid := str(st, "task_id")
	base := filepath.Dir(str(st, "output_dir"))
	writeVersion := func(rev string, files map[string]string) M {
		t.Helper()
		var data bytes.Buffer
		z := zip.NewWriter(&data)
		for name, content := range files {
			w, err := z.Create(name)
			check(err)
			_, err = io.WriteString(w, content)
			check(err)
		}
		check(z.Close())
		dir := filepath.Join(base, rev)
		file := filepath.Join(dir, "submission.zip")
		writeFile(file, data.Bytes(), 0600)
		return M{"output_dir": dir, "submission": file, "sha256": digest(file)}
	}
	old := writeVersion("r1", map[string]string{"src/main.go": "old\n", "tests/main.go": "same", "removed.txt": "gone"})
	now := writeVersion("r2", map[string]string{"src/main.go": "new\n", "tests/main.go": "same", "assets/logo.txt": "new", "empty/": ""})
	merge(st, now)
	st["revision_history"] = []M{old}
	st["revision"] = 3 // The writer is active, but the student can still read r2.
	a.saveTask(st)
	endpoint := "/api/workspace/task/" + tid
	tree := editorResult(t, editorCall(t, a, endpoint+"/submission-tree?revision=2", nil))
	if str(tree, "baseline") != "available" || !strings.Contains(string(jsonBytes(tree["directories"])), "empty") {
		t.Fatal(tree)
	}
	want := map[string]string{"src/main.go": "modified", "tests/main.go": "unchanged", "removed.txt": "removed", "assets/logo.txt": "added"}
	for _, row := range objects(tree["files"]) {
		name := str(row, "name")
		if want[name] != str(row, "change") {
			t.Fatal(row)
		}
		delete(want, name)
	}
	if len(want) != 0 {
		t.Fatal(want)
	}
	diff := editorResult(t, editorCall(t, a, endpoint+"/file-change?revision=2&name=src%2Fmain.go", nil))
	if !strings.Contains(str(diff, "lines"), "- old") || !strings.Contains(str(diff, "lines"), "+ new") {
		t.Fatal(diff)
	}
	if r := editorCall(t, a, endpoint+"/file-change?revision=2&name=..%2Fprivate", nil); r.Code != 409 {
		t.Fatal(r.Code)
	}
	if r := editorCall(t, a, endpoint+"/submission-tree?revision=3", nil); r.Code != 409 {
		t.Fatal("invented an unpublished revision")
	}
	writeFile(str(old, "submission"), []byte("tampered old bytes"), 0600)
	tree = editorResult(t, editorCall(t, a, endpoint+"/submission-tree?revision=2", nil))
	if str(tree, "baseline") != "unavailable" {
		t.Fatal(tree)
	}
	for _, row := range objects(tree["files"]) {
		if str(row, "change") != "unavailable" || row["previous"] != nil {
			t.Fatal(row)
		}
	}
	writeFile(str(now, "submission"), []byte("tampered current bytes"), 0600)
	if r := editorCall(t, a, endpoint+"/submission-tree?revision=2", nil); r.Code != 409 {
		t.Fatal("displayed changed bytes as frozen")
	}
}

func TestSubmissionTextEditReachesWriterAsNewRevision(t *testing.T) {
	a := testApp(t)
	st := revisionFixture(t, a)
	st["status"] = "awaiting"
	a.saveTask(st)
	tid := str(st, "task_id")
	submission := str(st, "submission")
	hash := digest(submission)
	endpoint := "/api/workspace/task/" + tid + "/file-edit"
	content := "package student\r\n"
	in := M{"id": strings.Repeat("d", 32), "revision": 2, "name": "src/main.go", "base_hash": hash, "text": content}
	for k, bad := range (M{"revision": 1, "name": "../report.md", "base_hash": "other"}) {
		original := in[k]
		in[k] = bad
		if r := editorCall(t, a, endpoint, in); r.Code != 409 {
			t.Fatal("accepted invalid edit", k)
		}
		in[k] = original
	}
	st["status"] = "submitted"
	a.saveTask(st)
	if r := editorCall(t, a, endpoint, in); r.Code != 409 {
		t.Fatal("changed submitted assignment")
	}
	st["status"] = "awaiting"
	a.saveTask(st)
	edited := editorResult(t, editorCall(t, a, endpoint, in))
	if str(edited, "status") != "revision_ready" || digest(submission) != hash {
		t.Fatal("changed the old version")
	}
	editorResult(t, editorCall(t, a, endpoint, in))
	in["text"] = "other"
	if r := editorCall(t, a, endpoint, in); r.Code != 409 {
		t.Fatal("overwrote accepted edit")
	}
	st = readMap(a.taskPath(tid))
	text := editorResult(t, editorCall(t, a, "/api/workspace/preview?"+url.Values{
		"course": {a.workspaceCourses()[0].ID}, "path": {filepath.ToSlash(relative(a.workspaceCourses()[0].Dir, submission))}, "member": {"src/main.go"}}.Encode(), nil))
	if str(text, "text") == content {
		t.Fatal("edited frozen member")
	}
	wrote, reviewed := false, false
	a.Upload = func(*School, M, string) M { t.Fatal("file editing uploaded an assignment"); return nil }
	a.RunModel = func(engine, job, prompt string, schema M, role string, plan M) M {
		if role == "writer" {
			wrote = true
			if string(readBytes(filepath.Join(job, "owner-file", "content.txt"))) != content || !strings.Contains(prompt, "src/main.go") {
				t.Fatal("missing named owner edit")
			}
			return resultAt(job, "3")
		}
		reviewed = true
		if exists(filepath.Join(job, "owner-file")) {
			t.Fatal("reviewer received editing session")
		}
		return M{"approved": true, "summary": "通过", "comments": []M{}, "checks": []string{"核对题目"}, "limitations": []string{}}
	}
	a.process(st)
	if !wrote || !reviewed || str(st, "status") != "awaiting" || number(st, "revision", 0) != 3 {
		t.Fatal("did not publish independently reviewed revision", st["status"], st["error"])
	}
}
