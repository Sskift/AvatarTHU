package app

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type submissionDirectoryMember struct {
	name string
	data []byte
	mode os.FileMode
}

func submissionDirectoryArchive(t *testing.T, entries ...submissionDirectoryMember) []byte {
	t.Helper()
	var data bytes.Buffer
	w := zip.NewWriter(&data)
	for _, entry := range entries {
		h := &zip.FileHeader{Name: entry.name, Method: zip.Deflate}
		h.SetMode(entry.mode)
		f, err := w.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(entry.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func submissionDirectoryWantFile(t *testing.T, name string, want []byte, mode os.FileMode) {
	t.Helper()
	got, err := os.ReadFile(name)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("file %s: content %q, error %v; want %q", name, got, err, want)
	}
	info, err := os.Lstat(name)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("file %s must be regular: %v", name, err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != mode.Perm() {
		t.Fatalf("file %s: mode %o, want %o", name, info.Mode().Perm(), mode.Perm())
	}
}

func TestMaterializeSubmissionExtractsOnlyOuterZIP(t *testing.T) {
	dir := t.TempDir()
	source, destination := filepath.Join(dir, "submission.zip"), filepath.Join(dir, "submission")
	native := submissionDirectoryArchive(t, submissionDirectoryMember{"inside.txt", []byte("native bytes"), 0644})
	entries := []submissionDirectoryMember{
		{"report.md", []byte("![result](images/result.png)"), 0644},
		{"images/result.png", []byte("image bytes"), 0644},
		{"program/run", []byte("#!/bin/sh\nprintf done\n"), 0755},
		{"program/source.zip", native, 0644},
		{"program/_internal/base_library.zip", native, 0644},
		{"report.docx", native, 0644},
		{"results.xlsx", native, 0644},
		{"program/app.jar", native, 0644},
	}
	writeFile(source, submissionDirectoryArchive(t, entries...), 0600)
	hash := digest(source)
	materializeSubmission(source, destination)
	for _, entry := range entries {
		submissionDirectoryWantFile(t, filepath.Join(destination, filepath.FromSlash(entry.name)), entry.data, entry.mode)
	}
	if exists(filepath.Join(destination, "inside.txt")) || exists(filepath.Join(destination, "program", "inside.txt")) {
		t.Fatal("materialization recursively unpacked a native or embedded archive")
	}
	if digest(source) != hash {
		t.Fatal("materialization changed the actual submission archive")
	}
	before, err := os.Stat(filepath.Join(destination, "program", "run"))
	check(err)
	materializeSubmission(source, destination)
	after, err := os.Stat(filepath.Join(destination, "program", "run"))
	check(err)
	if !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("identical existing directory was rewritten instead of reused")
	}
}

func TestMaterializeSubmissionCopiesSingleNativeFiles(t *testing.T) {
	for _, name := range []string{"answer.txt", "report.docx", "slides.pptx", "results.xlsx", "application.jar"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			source, destination := filepath.Join(dir, name), filepath.Join(dir, "submission")
			data := submissionDirectoryArchive(t, submissionDirectoryMember{"content.xml", []byte("native format"), 0644})
			writeFile(source, data, 0640)
			materializeSubmission(source, destination)
			submissionDirectoryWantFile(t, filepath.Join(destination, name), data, 0640)
			entries, err := os.ReadDir(destination)
			if err != nil || len(entries) != 1 {
				t.Fatalf("single-file submission gained files: %v, %v", entries, err)
			}
		})
	}
}

func TestMaterializeSubmissionPreservesExplicitEmptyDirectories(t *testing.T) {
	dir := t.TempDir()
	source, destination := filepath.Join(dir, "submission.zip"), filepath.Join(dir, "submission")
	writeFile(source, submissionDirectoryArchive(t,
		submissionDirectoryMember{"program/data/", nil, os.ModeDir | 0755},
		submissionDirectoryMember{"program/run", []byte("native executable"), 0755}), 0600)
	materializeSubmission(source, destination)
	empty := filepath.Join(destination, "program", "data")
	info, err := os.Lstat(empty)
	if err != nil || !info.IsDir() {
		t.Fatalf("runtime empty directory was not materialized: %v", err)
	}
	submissionDirectoryWantFile(t, filepath.Join(destination, "program", "run"), []byte("native executable"), 0755)
	check(os.Remove(empty))
	before := submissionDirectoryTree(t, destination)
	if err := attempt(func() { materializeSubmission(source, destination) }); err == nil {
		t.Fatal("missing explicit empty directory was accepted")
	}
	if got := submissionDirectoryTree(t, destination); got != before {
		t.Fatalf("rejection changed the existing runtime directory:\nbefore %s\nafter %s", before, got)
	}
}

func TestMaterializeSubmissionRejectsUnsafeArchivePaths(t *testing.T) {
	for _, name := range []string{"../outside.txt", "nested/../../outside.txt", "/outside.txt", `..\outside.txt`, `C:\outside.txt`, "C:/outside.txt", `\\server\share\outside.txt`, "file:stream", "bad\x00name"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			source, destination := filepath.Join(dir, "submission.zip"), filepath.Join(dir, "submission")
			outside := filepath.Join(dir, "outside.txt")
			writeFile(outside, []byte("untouched"), 0600)
			writeFile(source, submissionDirectoryArchive(t,
				submissionDirectoryMember{"valid.txt", []byte("valid"), 0644},
				submissionDirectoryMember{name, []byte("overwrite"), 0644}), 0600)
			if err := attempt(func() { materializeSubmission(source, destination) }); err == nil {
				t.Fatal("unsafe archive path accepted")
			}
			if exists(destination) {
				t.Fatal("failed archive left a partially materialized destination")
			}
			submissionDirectoryWantFile(t, outside, []byte("untouched"), 0600)
		})
	}
}

func TestMaterializeSubmissionRejectsAmbiguousArchiveMembers(t *testing.T) {
	for name, entries := range map[string][]submissionDirectoryMember{
		"duplicate":               {{"answer.txt", []byte("one"), 0644}, {"answer.txt", []byte("two"), 0644}},
		"duplicate-identical":     {{"answer.txt", []byte("same"), 0644}, {"answer.txt", []byte("same"), 0644}},
		"case-collision":          {{"Answer.txt", []byte("one"), 0644}, {"answer.txt", []byte("two"), 0644}},
		"file-before-child":       {{"program", []byte("file"), 0644}, {"program/main.go", []byte("source"), 0644}},
		"child-before-file":       {{"program/main.go", []byte("source"), 0644}, {"program", []byte("file"), 0644}},
		"case-file-directory":     {{"Program", []byte("file"), 0644}, {"program/main.go", []byte("source"), 0644}},
		"explicit-directory-file": {{"program/", nil, os.ModeDir | 0755}, {"program", []byte("file"), 0644}},
		"symlink":                 {{"link", []byte("../outside"), os.ModeSymlink | 0777}},
		"special-file":            {{"device", nil, os.ModeDevice | os.ModeCharDevice | 0600}},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			source, destination := filepath.Join(dir, "submission.zip"), filepath.Join(dir, "submission")
			writeFile(source, submissionDirectoryArchive(t, entries...), 0600)
			if err := attempt(func() { materializeSubmission(source, destination) }); err == nil {
				t.Fatal("ambiguous or non-regular ZIP member accepted")
			}
			if exists(destination) {
				t.Fatal("failed archive replaced the destination")
			}
		})
	}
}

func TestMaterializeSubmissionDoesNotOverwriteExistingDifferences(t *testing.T) {
	for _, change := range []string{"content", "missing", "extra", "file-instead-of-directory", "permissions"} {
		t.Run(change, func(t *testing.T) {
			if change == "permissions" && runtime.GOOS == "windows" {
				t.Skip("Windows does not retain POSIX executable permission bits")
			}
			dir := t.TempDir()
			source, destination := filepath.Join(dir, "submission.zip"), filepath.Join(dir, "submission")
			writeFile(source, submissionDirectoryArchive(t,
				submissionDirectoryMember{"report.md", []byte("report"), 0644},
				submissionDirectoryMember{"program/run", []byte("executable"), 0755}), 0600)
			materializeSubmission(source, destination)
			report := filepath.Join(destination, "report.md")
			run := filepath.Join(destination, "program", "run")
			switch change {
			case "content":
				writeFile(report, []byte("owner-edited report"), 0644)
			case "missing":
				check(os.Remove(report))
			case "extra":
				writeFile(filepath.Join(destination, "owner-notes.txt"), []byte("keep notes"), 0600)
			case "file-instead-of-directory":
				check(os.Remove(run))
				check(os.Remove(filepath.Dir(run)))
				writeFile(filepath.Dir(run), []byte("keep replacement"), 0600)
			case "permissions":
				check(os.Chmod(run, 0644))
			}
			before := submissionDirectoryTree(t, destination)
			if err := attempt(func() { materializeSubmission(source, destination) }); err == nil {
				t.Fatal("existing directory difference accepted")
			}
			if got := submissionDirectoryTree(t, destination); got != before {
				t.Fatalf("existing files changed after rejection:\nbefore %s\nafter %s", before, got)
			}
		})
	}
}

func submissionDirectoryTree(t *testing.T, root string) string {
	t.Helper()
	var entries []M
	err := filepath.WalkDir(root, func(name string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		r := M{"path": relative(root, name), "mode": info.Mode().String()}
		if info.Mode().IsRegular() {
			r["sha256"] = digest(name)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(name)
			if err != nil {
				return err
			}
			r["target"] = target
		}
		entries = append(entries, r)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(jsonBytes(entries))
}

func TestMaterializeSubmissionRejectsFilesystemSymlinks(t *testing.T) {
	for _, kind := range []string{"source", "destination", "destination-parent", "existing-member"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			source := filepath.Join(dir, "answer.txt")
			destination := filepath.Join(dir, "submission")
			outside := filepath.Join(dir, "outside")
			mkdir(outside)
			writeFile(source, []byte("answer"), 0644)
			writeFile(filepath.Join(outside, "answer.txt"), []byte("keep outside"), 0644)
			symlink := func(target, name string) {
				if err := os.Symlink(target, name); err != nil {
					t.Skipf("symbolic links unavailable: %v", err)
				}
			}
			switch kind {
			case "source":
				source = filepath.Join(dir, "source-link.txt")
				symlink(filepath.Join(outside, "answer.txt"), source)
			case "destination":
				symlink(outside, destination)
			case "destination-parent":
				link := filepath.Join(dir, "parent-link")
				symlink(outside, link)
				destination = filepath.Join(link, "submission")
			case "existing-member":
				mkdir(destination)
				symlink(filepath.Join(outside, "answer.txt"), filepath.Join(destination, "answer.txt"))
			}
			before := submissionDirectoryTree(t, dir)
			if err := attempt(func() { materializeSubmission(source, destination) }); err == nil {
				t.Fatal("filesystem symbolic link accepted")
			}
			if got := submissionDirectoryTree(t, dir); got != before {
				t.Fatalf("rejection modified existing files or links:\nbefore %s\nafter %s", before, got)
			}
		})
	}
}

func submissionDirectoryLegacyTask(t *testing.T) (*App, M) {
	t.Helper()
	a := testApp(t)
	st := fixture(t, a)
	out := filepath.Join(str(st, "assignment_dir"), "outputs", "r3")
	archive := submissionDirectoryArchive(t, submissionDirectoryMember{"src/main.go", []byte("package main\n"), 0644})
	artifact, submission, report := filepath.Join(out, "project.zip"), filepath.Join(out, "submission.zip"), filepath.Join(out, "review.md")
	writeFile(artifact, archive, 0600)
	writeFile(submission, archive, 0600)
	writeFile(report, []byte("preserved writer receipt"), 0600)
	merge(st, M{"revision": 3, "output_dir": out, "status": "awaiting", "ready": true,
		"artifacts":  []M{{"path": artifact, "sha256": digest(artifact)}},
		"submission": submission, "sha256": digest(submission), "report": report, "report_sha256": digest(report),
		"nonce": "legacy-nonce", "card_message_id": "legacy-card", "chat_id": "legacy-chat",
		"deliveries":       M{"document": M{"receipt": "original-receipt"}},
		"review_doc":       M{"url": "https://example.invalid/legacy", "verified": true},
		"revision_history": []M{{"revision": 2, "card_message_id": "older-card"}},
	})
	a.saveTask(st)
	for _, name := range []string{a.taskPath(str(st, "task_id")), filepath.Join(str(st, "assignment_dir"), "state.json")} {
		writeFile(name, append([]byte(" \n"), readBytes(name)...), 0600)
	}
	a.SendCard = func(M, string) M { t.Fatal("directory access sent a card"); return nil }
	a.Upload = func(*School, M, string) M { t.Fatal("directory access uploaded homework"); return nil }
	return a, st
}

func TestSubmissionFolderPreservesLegacyStateAndFrozenHashes(t *testing.T) {
	a, st := submissionDirectoryLegacyTask(t)
	tid := str(st, "task_id")
	paths := []string{a.taskPath(tid), filepath.Join(str(st, "assignment_dir"), "state.json"), str(st, "submission"), str(st, "report"), str(objects(st["artifacts"])[0], "path")}
	before := map[string][]byte{}
	for _, name := range paths {
		before[name] = readBytes(name)
	}
	for i := 0; i < 2; i++ {
		folder := a.submissionFolder(tid)
		if folder != filepath.Join(str(st, "output_dir"), "submission") {
			t.Fatalf("unexpected legacy submission directory: %s", folder)
		}
		submissionDirectoryWantFile(t, filepath.Join(folder, "src", "main.go"), []byte("package main\n"), 0644)
	}
	for name, want := range before {
		if !bytes.Equal(want, readBytes(name)) {
			t.Fatalf("opening legacy submission changed frozen bytes or state: %s", name)
		}
	}
	a.verifiedFiles(readMap(a.taskPath(tid)))
}

func TestSubmissionFolderVerifiesAllFrozenFilesBeforeExtraction(t *testing.T) {
	for _, kind := range []string{"artifact", "submission"} {
		t.Run(kind, func(t *testing.T) {
			a, st := submissionDirectoryLegacyTask(t)
			name := str(st, "submission")
			if kind == "artifact" {
				name = str(objects(st["artifacts"])[0], "path")
			}
			writeFile(name, []byte("tampered"), 0600)
			before := readBytes(a.taskPath(str(st, "task_id")))
			expectError(t, "产物已发生变化", func() { a.submissionFolder(str(st, "task_id")) })
			if exists(filepath.Join(str(st, "output_dir"), "submission")) {
				t.Fatal("tampered frozen files were extracted")
			}
			if !bytes.Equal(before, readBytes(a.taskPath(str(st, "task_id")))) {
				t.Fatal("failed directory access rewrote task state")
			}
		})
	}
}

func TestSubmissionSnapshotSeparatesDownloadsAndPreservesSingleFileUpload(t *testing.T) {
	for _, multiple := range []bool{false, true} {
		name := "single"
		if multiple {
			name = "multiple"
		}
		t.Run(name, func(t *testing.T) {
			a := testApp(t)
			st := fixture(t, a)
			job := filepath.Join(a.Root, "writer")
			r := resultAt(job, "answer")
			if multiple {
				writeFile(filepath.Join(job, "final", "project.zip"), submissionDirectoryArchive(t,
					submissionDirectoryMember{"program/run", []byte("executable"), 0755}), 0600)
				r["files"] = append(texts(r["files"]), "final/project.zip")
			}
			a.snapshot(st, r, job)
			out := str(st, "output_dir")
			for _, artifact := range objects(st["artifacts"]) {
				file := str(artifact, "path")
				if filepath.Dir(file) != filepath.Join(out, "artifacts") {
					t.Fatalf("download is not under artifacts/: %s", file)
				}
				if exists(filepath.Join(out, filepath.Base(file))) {
					t.Fatalf("download still duplicated at revision root: %s", file)
				}
			}
			if str(st, "report") != filepath.Join(out, "review.md") || !exists(str(st, "report")) {
				t.Fatal("writer review was moved out of the revision root")
			}
			if multiple {
				if str(st, "submission") != filepath.Join(out, "submission.zip") {
					t.Fatal("multiple deliverables lost the actual submission archive")
				}
			} else if str(st, "submission") != str(objects(st["artifacts"])[0], "path") || exists(filepath.Join(out, "submission.zip")) {
				t.Fatal("single non-ZIP deliverable was unnecessarily repackaged")
			}
			folder := a.submissionFolder(str(st, "task_id"))
			if !bytes.Equal(readBytes(filepath.Join(folder, "答案.txt")), []byte("answer")) {
				t.Fatal("submission directory lost the actual answer")
			}
			if exists(filepath.Join(folder, "artifacts")) || exists(filepath.Join(folder, "review.md")) || exists(filepath.Join(folder, "project.zip")) {
				t.Fatal("editing downloads or writer review leaked into submission directory")
			}
			if multiple {
				submissionDirectoryWantFile(t, filepath.Join(folder, "program", "run"), []byte("executable"), 0755)
			}
			a.verifiedFiles(readMap(a.taskPath(str(st, "task_id"))))
		})
	}
}

func TestFilesCommandPrintsSubmissionDirectoryWithoutChangingTask(t *testing.T) {
	a, st := submissionDirectoryLegacyTask(t)
	t.Setenv("AVATARTHU_HOME", a.Root)
	tid := str(st, "task_id")
	before := readBytes(a.taskPath(tid))
	reader, writer, err := os.Pipe()
	check(err)
	original := os.Stdout
	os.Stdout = writer
	defer func() { os.Stdout = original }()
	code := Main([]string{"files", tid})
	check(writer.Close())
	os.Stdout = original
	output, err := io.ReadAll(reader)
	check(err)
	check(reader.Close())
	if code != 0 || strings.TrimSpace(string(output)) != filepath.Join(str(st, "output_dir"), "submission") {
		t.Fatalf("files command: code %d, output %q", code, output)
	}
	if !bytes.Equal(before, readBytes(a.taskPath(tid))) {
		t.Fatal("files command changed the task or current-version card")
	}
}
