package app

import (
	"bytes"
	"context"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
)

func layoutFixture(t *testing.T) (*App, M) {
	a := testApp(t)
	st := fixture(t, a)
	base := filepath.Join(filepath.Dir(str(st, "assignment_dir")), "lab-1")
	st["assignment_dir"] = base
	st["folder"] = assignmentPath(base, "source")
	writeFile(filepath.Join(str(st, "folder"), "question.txt"), []byte("required answer"), 0600)
	writeJSON(assignmentPath(base, "assignment.json"), st)
	return a, st
}

func TestAssignmentHasFourRootEntriesAndPreservesModifiedFinal(t *testing.T) {
	a, st := layoutFixture(t)
	base := str(st, "assignment_dir")
	job := assignmentPath(base, "runs", "r1", "writer")
	r := resultAt(job, "answer")
	a.snapshot(st, r, job)
	entries, err := os.ReadDir(base)
	check(err)
	names := []string{}
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	if !reflect.DeepEqual(names, []string{"presentation", "submission", "submission.zip", "workspace"}) {
		t.Fatal(names)
	}
	before := readBytes(a.taskPath(str(st, "task_id")))
	folder := a.submissionFolder(str(st, "task_id"))
	if folder != filepath.Join(base, "submission") || a.exportSubmission(str(st, "task_id"), "") != filepath.Join(base, "submission.zip") {
		t.Fatal("final entry mismatch")
	}
	if !bytes.Equal(before, readBytes(a.taskPath(str(st, "task_id")))) {
		t.Fatal("inspection changed task/card state")
	}
	answer := filepath.Join(folder, "答案.txt")
	writeFile(answer, []byte("owner edit"), 0600)
	st["revision"] = 2
	next := assignmentPath(base, "runs", "r2", "writer")
	newer := resultAt(next, "new answer")
	inMemory := fingerprint(st)
	expectError(t, "已被修改", func() { a.snapshot(st, newer, next) })
	if fingerprint(st) != inMemory {
		t.Fatal("failed publication changed the scheduler's current task")
	}
	if !bytes.Equal(before, readBytes(a.taskPath(str(st, "task_id")))) {
		t.Fatal("failed publication changed durable task/card state")
	}
	if string(readBytes(answer)) != "owner edit" {
		t.Fatal("owner change overwritten")
	}
}

func TestAssignmentFinalAdvancesAndKeepsPreviousVersion(t *testing.T) {
	a, st := layoutFixture(t)
	base := str(st, "assignment_dir")
	for version, answer := range []string{"one", "two"} {
		st["revision"] = version + 1
		job := assignmentPath(base, "runs", string(rune('a'+version)))
		a.snapshot(st, resultAt(job, answer), job)
	}
	if string(readBytes(filepath.Join(base, "submission", "答案.txt"))) != "two" {
		t.Fatal("stale final")
	}
	prior, _ := filepath.Glob(assignmentPath(base, "submission-history", "*", "submission", "答案.txt"))
	if len(prior) != 1 || string(readBytes(prior[0])) != "one" {
		t.Fatal("previous final missing")
	}
}

func TestManualFinalMatchesCLIWithoutChangingFrozenSubmission(t *testing.T) {
	a, st := layoutFixture(t)
	base := str(st, "assignment_dir")
	job := assignmentPath(base, "runs", "writer")
	a.snapshot(st, resultAt(job, "answer"), job)
	before := readBytes(a.taskPath(str(st, "task_id")))
	source := assignmentPath(base, "exports", "minimal.zip")
	writeFile(source, submissionTestArchive(t, map[string][]byte{"report.pdf": []byte("report")}), 0600)
	record := readMap(assignmentPath(base, "submission.json"))
	merge(record, M{"manual": true, "source": source, "source_sha256": digest(source)})
	writeJSON(assignmentPath(base, "submission.json"), record)
	folder := a.submissionFolder(str(st, "task_id"))
	if len(visibleFiles(folder)) != 1 || string(readBytes(filepath.Join(folder, "report.pdf"))) != "report" {
		t.Fatal("manual selection lost")
	}
	if !bytes.Equal(before, readBytes(a.taskPath(str(st, "task_id")))) {
		t.Fatal("manual final changed frozen state")
	}
	a.verifiedFiles(st)
}

func TestLegacyRootLinkPreservesMaterialsAndPresentationAccess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("legacy macOS symlink migration")
	}
	a := testApp(t)
	st := frozen(t, a)
	old := str(st, "assignment_dir")
	before := materialsHash(st)
	taskBefore := readBytes(a.taskPath(str(st, "task_id")))
	root := filepath.Join(filepath.Dir(old), "lab-1")
	legacy := filepath.Join(root, "workspace", "legacy")
	mkdir(filepath.Dir(legacy))
	check(os.Rename(old, legacy))
	check(os.Symlink(legacy, old))
	writeJSON(filepath.Join(legacy, ".layout.json"), M{"root": root})
	for _, kind := range []string{"source", "outputs"} {
		target := assignmentPath(old, kind)
		mkdir(filepath.Dir(target))
		check(os.Rename(filepath.Join(legacy, kind), target))
		check(os.Symlink(target, filepath.Join(legacy, kind)))
	}
	if before != materialsHash(st) {
		t.Fatal("legacy material fingerprint changed")
	}
	if a.submissionFolder(str(st, "task_id")) != filepath.Join(root, "submission") {
		t.Fatal("old task did not resolve new final")
	}
	if !bytes.Equal(taskBefore, readBytes(a.taskPath(str(st, "task_id")))) {
		t.Fatal("migration inspection changed state")
	}
	c := a.workspaceCourses()[0]
	found := false
	for _, f := range a.workspaceFiles(c) {
		if str(f, "name") == "答案.txt" {
			found = true
		}
	}
	if !found {
		t.Fatal("presentation files disappeared")
	}
	newCourse := filepath.Join(filepath.Dir(c.Dir), "course-12345678")
	check(os.Rename(c.Dir, newCourse))
	check(os.Symlink(newCourse, c.Dir))
	writeJSON(filepath.Join(newCourse, ".course-layout.json"), M{"identity_dir": c.Dir})
	if current := a.workspaceCourses(); len(current) != 1 || current[0].ID != c.ID || len(a.workspaceFiles(current[0])) == 0 {
		t.Fatal("course rename changed workspace identity or hid files")
	}
	for _, p := range []string{"homework/lab-1/workspace/runs/log.txt", "homework/lab-1/workspace/legacy/state.json", "homework/lab-1/workspace/submission.json"} {
		if publicCourseFile(p) != "" {
			t.Fatal("private workspace exposed")
		}
	}
}

func TestEnglishNamesKeepExtensionsAndAvoidCollisions(t *testing.T) {
	for _, name := range []string{"Lab 1", "实验报告.pdf", "补充材料.zip"} {
		got := englishName(name)
		for _, r := range got {
			if r > 127 {
				t.Fatal(got)
			}
		}
		if filepath.Ext(got) != filepath.Ext(name) {
			t.Fatal("extension lost", got)
		}
	}
	if englishName("报告.pdf") == englishName("答案.pdf") {
		t.Fatal("translated paths collide")
	}
}

func TestExistingAssignmentLayout(t *testing.T) {
	home := os.Getenv("AVATARTHU_LAYOUT_SMOKE_HOME")
	if home == "" {
		t.Skip("optional local migration acceptance")
	}
	a := New(context.Background(), home)
	state := M{}
	for _, st := range a.tasks() {
		tid := str(st, "task_id")
		a.verifiedFiles(st)
		task := a.workspaceTask(tid)
		state[tid] = M{"state": digest(a.taskPath(tid)), "materials": materialsHash(st), "course_id": task["course_id"], "attachments": len(objects(task["attachments"])), "revisions": task["revisions"]}
	}
	// Absolute artifact URLs change after physical relocation; compare each
	// version's filenames and count, while frozen file bytes remain verified.
	for _, v := range state {
		row := v.(M)
		versions := []any{}
		for _, r := range objects(row["revisions"]) {
			names := []string{}
			for _, f := range objects(r["files"]) {
				names = append(names, str(f, "name"))
			}
			sort.Strings(names)
			versions = append(versions, []any{r["revision"], names})
		}
		row["revisions"] = versions
	}
	baseline := filepath.Join(a.Root, "work", "maintenance", "four-part-layout-baseline.json")
	if os.Getenv("AVATARTHU_LAYOUT_SMOKE_MODE") == "before" {
		writeJSON(baseline, state)
		return
	}
	if fingerprint(readMap(baseline)) != fingerprint(state) {
		t.Fatal("task bytes, material hashes, course IDs or displayed files changed")
	}
	for _, st := range a.tasks() {
		root := assignmentRoot(str(st, "assignment_dir"))
		for _, r := range root {
			if r > 127 {
				t.Fatal("new assignment entry is not English", root)
			}
		}
		folder := a.submissionFolder(str(st, "task_id"))
		if folder != filepath.Join(root, "submission") {
			t.Fatal("files returned stale entry")
		}
		entries, err := os.ReadDir(root)
		check(err)
		names := []string{}
		for _, e := range entries {
			if !strings.HasPrefix(e.Name(), ".") {
				names = append(names, e.Name())
			}
		}
		sort.Strings(names)
		if !reflect.DeepEqual(names, []string{"presentation", "submission", "submission.zip", "workspace"}) {
			t.Fatal(root, names)
		}
		t.Logf("%s: four entries, frozen hashes and workspace visibility verified", str(st, "task_id"))
	}
	for _, c := range a.workspaceCourses() {
		for _, f := range a.workspaceFiles(c) {
			p := str(f, "path")
			if !strings.Contains(p, "/presentation/") && !strings.Contains(p, "/workspace/source/") {
				continue
			}
			res := editorCall(t, a, "/api/workspace/file?"+url.Values{"course": {c.ID}, "path": {p}}.Encode(), nil)
			if res.Code != 200 || !bytes.Equal(res.Body.Bytes(), readBytes(filepath.Join(c.Dir, filepath.FromSlash(p)))) {
				t.Fatal("migrated file cannot be downloaded", p, res.Code)
			}
		}
	}
}
