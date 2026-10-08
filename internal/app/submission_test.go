package app

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func submissionTestArchive(t *testing.T, members map[string][]byte) []byte {
	t.Helper()
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for name, content := range members {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		header.SetMode(0755)
		f, err := w.CreateHeader(header)
		check(err)
		_, err = f.Write(content)
		check(err)
	}
	check(w.Close())
	return b.Bytes()
}

func TestSubmissionUnpacksArchivesAndPreservesFiles(t *testing.T) {
	dir := t.TempDir()
	report := []byte("![image](screenshots/result.png)")
	nested := submissionTestArchive(t, map[string][]byte{"main.js": []byte("run()")})
	project := submissionTestArchive(t, map[string][]byte{"program/run": []byte("executable"), "program/source.zip": nested, "program/_internal/base_library.zip": nested})
	source := submissionTestArchive(t, map[string][]byte{"report.md": report, "screenshots/result.png": []byte("image")})
	office := submissionTestArchive(t, map[string][]byte{"word/document.xml": []byte("document")})
	files := []string{}
	for name, data := range map[string][]byte{"project.zip": project, "report-source.zip": source, "report.md": report, "report.docx": office} {
		file := filepath.Join(dir, name)
		writeFile(file, data, 0600)
		files = append(files, file)
	}
	destination := filepath.Join(dir, "submission.zip")
	buildSubmissionZIP(destination, files)
	z, err := zip.OpenReader(destination)
	check(err)
	defer z.Close()
	want := map[string][]byte{"program/run": []byte("executable"), "program/main.js": []byte("run()"), "program/_internal/base_library.zip": nested, "report.md": report, "screenshots/result.png": []byte("image"), "report.docx": office}
	if len(z.File) != len(want) {
		t.Fatalf("unexpected number of files: %d", len(z.File))
	}
	for _, f := range z.File {
		r, err := f.Open()
		check(err)
		data, err := io.ReadAll(r)
		check(err)
		r.Close()
		if !bytes.Equal(want[f.Name], data) {
			t.Fatalf("unexpected content: %s", f.Name)
		}
		if f.Name == "program/run" && f.Mode().Perm()&0111 == 0 {
			t.Fatal("executable permission lost")
		}
	}
}

func TestSubmissionRejectsUnsafeAndConflictingMembers(t *testing.T) {
	for _, name := range []string{"../outside.txt", "/outside.txt", `..\outside.txt`, "C:/outside.txt"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			file := filepath.Join(dir, "project.zip")
			writeFile(file, submissionTestArchive(t, map[string][]byte{name: []byte("bad")}), 0600)
			expectError(t, "无效路径", func() { buildSubmissionZIP(filepath.Join(dir, "submission.zip"), []string{file}) })
		})
	}
	dir := t.TempDir()
	one, two, dest := filepath.Join(dir, "one.zip"), filepath.Join(dir, "two.zip"), filepath.Join(dir, "submission.zip")
	writeFile(one, submissionTestArchive(t, map[string][]byte{"report.md": []byte("one")}), 0600)
	writeFile(two, submissionTestArchive(t, map[string][]byte{"report.md": []byte("two")}), 0600)
	writeFile(dest, []byte("previous package"), 0600)
	expectError(t, "内容不同的同名文件", func() { buildSubmissionZIP(dest, []string{one, two}) })
	if string(readBytes(dest)) != "previous package" {
		t.Fatal("failed packaging replaced the previous package")
	}
}

func TestSingleArchiveSnapshotAndExportPreservePublishedState(t *testing.T) {
	a := testApp(t)
	st := fixture(t, a)
	job := filepath.Join(a.Root, "writer")
	r := resultAt(job, "2")
	inner := submissionTestArchive(t, map[string][]byte{"answer.txt": []byte("2")})
	file := filepath.Join(job, "final", "project.zip")
	writeFile(file, submissionTestArchive(t, map[string][]byte{"source.zip": inner}), 0600)
	r["files"] = []string{"final/project.zip"}
	a.snapshot(st, r, job)
	if filepath.Base(str(st, "submission")) != "submission.zip" {
		t.Fatal("single archive bypassed unpacking")
	}
	merge(st, M{"status": "awaiting", "card_message_id": "unchanged-card"})
	a.saveTask(st)
	before := readBytes(a.taskPath(str(st, "task_id")))
	original := digest(str(st, "submission"))
	exported := a.exportSubmission(str(st, "task_id"), "project.zip")
	if !bytes.Equal(before, readBytes(a.taskPath(str(st, "task_id")))) || original != digest(str(st, "submission")) {
		t.Fatal("export changed published state or card-bound bytes")
	}
	z, err := zip.OpenReader(exported)
	check(err)
	defer z.Close()
	if len(z.File) != 1 || z.File[0].Name != "answer.txt" {
		t.Fatal("export still contains nested archives")
	}
	for _, artifact := range objects(st["artifacts"]) {
		if !strings.HasSuffix(str(artifact, "path"), "project.zip") {
			t.Fatal("standalone download was lost")
		}
	}
	if _, err := os.Stat(exported); err != nil {
		t.Fatal(err)
	}
}

func TestSubmissionSelectionSeparatesPresentationArtifacts(t *testing.T) {
	for _, program := range []bool{false, true} {
		t.Run(fmt.Sprint(program), func(t *testing.T) {
			a := testApp(t)
			st := fixture(t, a)
			job := filepath.Join(a.Root, "writer")
			r := resultAt(job, "answer")
			writeFile(filepath.Join(job, "final", "report.pdf"), []byte("report with embedded figures"), 0644)
			writeFile(filepath.Join(job, "final", "report-source.zip"), submissionTestArchive(t, map[string][]byte{"report.md": []byte("editable"), "images/plot.png": []byte("image")}), 0600)
			r["files"] = []string{"final/report.pdf", "final/report-source.zip"}
			r["submission_files"] = []string{"report.pdf"}
			if program {
				writeFile(filepath.Join(job, "final", "project.zip"), submissionTestArchive(t, map[string][]byte{"requirements.txt": []byte("deps"), "timer_iotdb/lab1_inject.py": []byte("complete code")}), 0600)
				r["files"] = append(texts(r["files"]), "final/project.zip")
				r["submission_files"] = append(texts(r["submission_files"]), "project.zip")
			}
			r = validateWriter(r, job)
			a.snapshot(st, r, job)
			folder := filepath.Join(a.outputDir(st), "submission")
			submissionDirectoryWantAbsent(t, filepath.Join(folder, "report.md"))
			submissionDirectoryWantAbsent(t, filepath.Join(folder, "images"))
			if len(visibleFiles(folder)) != map[bool]int{false: 1, true: 3}[program] {
				t.Fatal("submission contains non-selected presentation material")
			}
			if len(objects(st["artifacts"])) != len(texts(r["files"])) {
				t.Fatal("separate editing download was lost")
			}
			if !program && filepath.Ext(str(st, "submission")) != ".pdf" {
				t.Fatal("single PDF upload was unnecessarily repackaged")
			}
		})
	}
}

func TestSubmissionSelectionRejectsMissingDuplicateOrEmptyFiles(t *testing.T) {
	for _, selected := range [][]string{{}, {"missing.pdf"}, {"答案.txt", "final/答案.txt"}} {
		a := testApp(t)
		job := filepath.Join(a.Root, "writer")
		r := resultAt(job, "answer")
		r["submission_files"] = selected
		expectError(t, "提交文件", func() { validateWriter(r, job) })
	}
}

func TestSubmissionSelectionRequiresFreshReviewWhenSelectionChanges(t *testing.T) {
	a := testApp(t)
	st := fixture(t, a)
	job := filepath.Join(a.Root, "writer")
	r := resultAt(job, "answer")
	writeFile(filepath.Join(job, "final", "notes.txt"), []byte("supporting material"), 0600)
	r["files"] = append(texts(r["files"]), "final/notes.txt")
	r["submission_files"] = []string{"final/答案.txt"}
	calls := 0
	a.RunModel = func(_ string, reviewJob, _ string, _ M, role string, _ M) M {
		calls++
		manifest := texts(readMap(filepath.Join(reviewJob, "submission-files.json"))["files"])
		if role != "reviewer" || !reflect.DeepEqual(manifest, texts(r["submission_files"])) {
			t.Fatal("review did not receive the actual submission selection")
		}
		if exists(filepath.Join(reviewJob, "review.md")) {
			t.Fatal("reviewer received writer self-assessment")
		}
		return M{"approved": true, "summary": "checked", "comments": []M{}, "checks": []string{"read candidate"}, "limitations": []string{}}
	}
	plan := M{"writer": "codex", "reviewer": "codex"}
	a.review(st, r, job, plan)
	a.review(st, r, job, plan)
	if calls != 1 {
		t.Fatal("unchanged review was not reused")
	}
	r["submission_files"] = texts(r["files"])
	a.review(st, r, job, plan)
	if calls != 2 || len(objects(st["review_history"])) != 2 {
		t.Fatal("changed submission selection reused a stale review")
	}
}
