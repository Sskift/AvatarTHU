package app

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestWorkspaceDirectoryMatchesAssignmentLayout(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(map[bool]string{false: "current", true: "legacy"}[legacy], func(t *testing.T) {
			a := testApp(t)
			st := fixture(t, a)
			if !legacy {
				root := str(st, "assignment_dir")
				mkdir(filepath.Join(root, "workspace"))
				check(os.Rename(str(st, "folder"), filepath.Join(root, "workspace", "source")))
				st["folder"] = filepath.Join(root, "workspace", "source")
			}
			job := filepath.Join(a.Root, "fixture-job")
			result := resultAt(job, "2")
			writeFile(filepath.Join(job, "final", "program.txt"), []byte("program"), 0600)
			result["files"] = append(texts(result["files"]), "final/program.txt")
			a.snapshot(st, result, job)
			a.publishSubmission(st)
			a.saveTask(st)
			root := str(st, "assignment_dir")
			writeFile(assignmentPath(root, "report-source", "r1", "report.md"), []byte("editable report"), 0600)
			c := a.workspaceCourses()[0]
			base := "homework/" + filepath.Base(root)
			before, frozen := digest(a.taskPath(str(st, "task_id"))), digest(str(st, "submission"))
			get := func(p string) M {
				return editorResult(t, editorCall(t, a, "/api/workspace/directory?"+url.Values{"course": {c.ID}, "path": {p}}.Encode(), nil))
			}
			names := []string{}
			for _, entry := range objects(get(base)["entries"]) {
				names = append(names, str(entry, "name"))
			}
			if !reflect.DeepEqual(names, []string{"submission", "submission.zip", "workspace", "presentation"}) {
				t.Fatal(names)
			}
			for _, entry := range objects(get("")["entries"]) {
				if str(entry, "kind") != "directory" {
					t.Fatal("course root was flattened", entry)
				}
			}
			if names := objects(get(base + "/workspace")["entries"]); len(names) != 1 || str(names[0], "name") != "source" {
				t.Fatal(names)
			}
			source := objects(get(base + "/workspace/source")["entries"])
			if len(source) != 1 || str(source[0], "name") != "question.txt" {
				t.Fatal(source)
			}
			for _, p := range []string{base, base + "/submission", base + "/presentation/report-source/r1"} {
				for _, file := range objects(get(p)["entries"]) {
					if str(file, "kind") == "directory" {
						continue
					}
					r := editorCall(t, a, "/api/workspace/file?"+url.Values{"course": {c.ID}, "path": {str(file, "path")}}.Encode(), nil)
					if r.Code != 200 || !bytes.Equal(r.Body.Bytes(), readBytes(a.workspaceFile(c, str(file, "path")))) {
						t.Fatal("directory download failed", r.Code, file)
					}
				}
			}
			if digest(a.taskPath(str(st, "task_id"))) != before || digest(str(st, "submission")) != frozen {
				t.Fatal("browsing changed frozen state")
			}
		})
	}
}

func TestWorkspaceDirectoryKeepsPrivateFilesAndLinksOut(t *testing.T) {
	a, st := editorFixture(t)
	c := a.workspaceCourses()[0]
	base := "homework/" + filepath.Base(str(st, "assignment_dir"))
	writeFile(filepath.Join(c.Dir, "courseware", ".private.json"), []byte("secret"), 0600)
	writeFile(assignmentPath(str(st, "assignment_dir"), "runs", "writer", "chat.txt"), []byte("private conversation"), 0600)
	for _, p := range []string{"../", "courseware/..", "/courseware", "courseware\\user", base + "/workspace/runs", base + "/workspace/state.json"} {
		r := editorCall(t, a, "/api/workspace/directory?"+url.Values{"course": {c.ID}, "path": {p}}.Encode(), nil)
		if r.Code == 200 {
			t.Fatal("private path browsable", p)
		}
	}
	if e := os.Symlink(str(st, "submission"), filepath.Join(c.Dir, "courseware", "linked.zip")); e == nil {
		result := a.workspaceDirectory(c, "courseware")
		if strings.Contains(string(jsonBytes(result)), "linked.zip") {
			t.Fatal("symlink offered for download")
		}
	}
	r := httptest.NewRecorder()
	a.editorHandler("secret", "127.0.0.1:7777").ServeHTTP(r, httptest.NewRequest(http.MethodGet, "http://127.0.0.1:7777/api/workspace/directory?course="+c.ID, nil))
	if r.Code != http.StatusUnauthorized {
		t.Fatal("unauthenticated directory access", r.Code)
	}
}

func TestWorkspaceDirectoryUsesCoursewareTitles(t *testing.T) {
	a, _ := editorFixture(t)
	c := a.workspaceCourses()[0]
	writeFile(filepath.Join(c.Dir, "courseware", "abc123", "file.pdf"), []byte("pdf"), 0600)
	writeJSON(filepath.Join(c.Dir, "courseware", "index.json"), []M{{"path": "abc123/file.pdf", "title": "Lecture 1"}})
	entries := objects(a.workspaceDirectory(c, "courseware/abc123")["entries"])
	if len(entries) != 1 || str(entries[0], "title") != "Lecture 1" || str(entries[0], "path") != "courseware/abc123/file.pdf" {
		t.Fatal("courseware name or download path lost", entries)
	}
	crumbs := objects(a.workspaceDirectory(c, "courseware/abc123")["breadcrumbs"])
	if str(crumbs[len(crumbs)-1], "title") != "Lecture 1" {
		t.Fatal(crumbs)
	}
}
