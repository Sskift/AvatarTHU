package app

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func completedReview(approved bool) M {
	return M{"approved": approved, "summary": "核对本轮文件", "comments": []M{}, "checks": []string{}, "limitations": []string{}}
}

func TestMaterialSnapshotPreservesOriginalReadmes(t *testing.T) {
	a := testApp(t)
	st := fixture(t, a)
	root := filepath.Join(str(st, "folder"), "README.md")
	nested := filepath.Join(str(st, "folder"), "project", "README.md")
	course := filepath.Join(str(st, "courseware"), "README.md")
	writeFile(root, []byte(str(st, "description")), 0600)
	writeFile(nested, []byte("project requirements"), 0600)
	writeFile(course, []byte("course environment"), 0600)
	first := filepath.Join(a.Root, "first")
	freezeMaterials(st, first)
	if exists(filepath.Join(first, "attachments", "README.md")) || digest(nested) != digest(filepath.Join(first, "attachments", "project", "README.md")) || digest(course) != digest(filepath.Join(first, "courseware", "README.md")) {
		t.Fatal("did not distinguish generated description from original readmes")
	}
	writeFile(root, []byte("additional assignment requirements"), 0600)
	second := filepath.Join(a.Root, "second")
	freezeMaterials(st, second)
	if digest(root) != digest(filepath.Join(second, "attachments", "README.md")) {
		t.Fatal("lost distinct root attachment")
	}
}

func TestReviewAndFreezeSameSubmission(t *testing.T) {
	a := testApp(t)
	st := fixture(t, a)
	job := filepath.Join(a.Root, "writer")
	a.prepare(st, job)
	// The writer can use its local copies however it wants. An independent
	// reviewer must always get the assignment originally captured by AvatarTHU.
	writeFile(filepath.Join(job, "input", "assignment.md"), []byte("writer's interpretation"), 0600)
	writeFile(filepath.Join(job, "input", "writer-private.txt"), []byte("private note"), 0600)
	r := resultAt(job, "answer")
	writeFile(filepath.Join(job, "final", "bin", "run"), []byte("runnable"), 0755)
	writeFile(filepath.Join(job, "final", "notes.md"), []byte("editing notes"), 0600)
	r["files"] = append(texts(r["files"]), "final/bin/run", "final/notes.md")
	r["submission_files"] = []string{"final/bin/run", "final/答案.txt"}
	a.RunModel = func(_, reviewJob, _ string, _ M, _ string, _ M) M {
		if string(readBytes(filepath.Join(reviewJob, "input", "assignment.md"))) != str(st, "description") || exists(filepath.Join(reviewJob, "input", "writer-private.txt")) {
			t.Fatal("reviewer saw the writer's modified materials")
		}
		exe := filepath.Join(reviewJob, "submission", "bin", "run")
		if !exists(exe) || runtime.GOOS != "windows" && artifactMode(exe)&0111 == 0 || exists(filepath.Join(reviewJob, "submission", "notes.md")) {
			t.Fatal("reviewer did not receive selected files with their paths and permissions")
		}
		result := completedReview(true)
		result["comments"] = []M{{"artifact": "run", "location": "line 1", "comment": "check result", "suggestion": "run example"}}
		return result
	}
	decision := a.review(st, r, job, a.pairing())
	if !strings.Contains(reviewFeedback(decision), "run · line 1") {
		t.Fatal("writer feedback lost its artifact reference")
	}
	writeFile(filepath.Join(job, "final", "bin", "run"), []byte("unreviewed writer edit"), 0600)
	a.snapshot(st, r, job, parseMap(jsonBytes(decision)))
	if str(st, "sha256") != str(decision, "submission_sha256") || str(st, "sha256") != digest(str(st, "submission")) {
		t.Fatal("frozen upload differs from the independent review")
	}
	for _, artifact := range objects(st["artifacts"]) {
		if boolean(artifact, "in_submission") == (str(artifact, "source_path") == "final/notes.md") {
			t.Fatal("frozen download lost its selection", artifact)
		}
	}
	if str(st, "sha256") != digest(a.exportSubmission(str(st, "task_id"), "")) {
		t.Fatal("legacy layout exported a different upload")
	}
	check(os.RemoveAll(job))
	merge(st, M{"revision": 2, "review_round": 1, "previous_job": job})
	next := filepath.Join(a.Root, "next-writer")
	a.prepare(st, next)
	if string(readBytes(filepath.Join(next, "previous-final", "bin", "run"))) != "runnable" || !exists(filepath.Join(next, "previous-final", "notes.md")) {
		t.Fatal("revision did not use the frozen downloads after removal of the writer's run")
	}
}

func TestReviewRejectsChangedSubmissionOrUnboundComment(t *testing.T) {
	for _, which := range []string{"submission", "input", "comment"} {
		t.Run(which, func(t *testing.T) {
			a := testApp(t)
			st := fixture(t, a)
			job := filepath.Join(a.Root, "writer")
			a.prepare(st, job)
			r := resultAt(job, "answer")
			a.RunModel = func(_, reviewJob, _ string, _ M, _ string, _ M) M {
				value := completedReview(true)
				switch which {
				case "submission":
					writeFile(filepath.Join(reviewJob, "submission", "extra.txt"), []byte("extra"), 0600)
				case "input":
					writeFile(filepath.Join(reviewJob, "input", "assignment.md"), []byte("changed requirement"), 0600)
				case "comment":
					value["comments"] = []M{{"artifact": "stale.pdf", "location": "p1", "comment": "problem", "suggestion": "fix"}}
				}
				return value
			}
			if err := attempt(func() { a.review(st, r, job, a.pairing()) }); err == nil || len(objects(st["review_history"])) != 0 {
				t.Fatal("accepted a verdict detached from its reviewed inputs")
			}
		})
	}
}

func TestBrokenDeliveryReturnsToWriterAndPreservesVerdicts(t *testing.T) {
	for _, fix := range []bool{false, true} {
		name := "round_limit"
		if fix {
			name = "repaired"
		}
		t.Run(name, func(t *testing.T) {
			a := testApp(t)
			a.saveConfig(M{"lark_enabled": false, "max_review_rounds": 2})
			st := fixture(t, a)
			writers := 0
			a.RunModel = func(_, job, prompt string, _ M, role string, _ M) M {
				if role == "writer" {
					writers++
					r := resultAt(job, "answer")
					if writers == 2 && !strings.Contains(prompt, "project.zip") {
						t.Fatal("packaging failure not returned to the writer")
					}
					var data []byte
					if !fix || writers == 1 {
						data = []byte("invalid project archive")
					} else {
						data = submissionTestArchive(t, map[string][]byte{"src/main.go": []byte("package main")})
					}
					writeFile(filepath.Join(job, "final", "project.zip"), data, 0600)
					r["files"] = []string{"final/project.zip"}
					r["submission_files"] = r["files"]
					return r
				}
				if !fix || writers == 1 {
					if exists(filepath.Join(job, "candidate", "project.zip")) || str(readMap(filepath.Join(job, "submission-files.json")), "packaging_error") == "" {
						t.Fatal("unreadable archive sent to reviewer without diagnostics")
					}
				}
				return completedReview(true) // Keep even a positive verdict separate from a packaging error.
			}
			a.process(st)
			history := objects(st["review_history"])
			if writers != 2 || len(history) != 2 || !boolean(history[0], "approved") || str(history[0], "packaging_error") == "" {
				t.Fatal("lost independent review or packaging diagnosis")
			}
			if fix {
				if str(st, "status") != "awaiting" || str(st, "sha256") == "" {
					t.Fatal("did not freeze repaired and reviewed upload")
				}
			} else {
				if str(st, "status") != "needs_student" || str(st, "submission") != "" || !strings.Contains(string(readBytes(str(st, "local_review"))), "project.zip") {
					t.Fatal("published an invalid upload or lost the review document")
				}
				tid := str(st, "task_id")
				expectError(t, "还没有冻结的提交文件", func() { a.exportSubmission(tid, "") })
				download := a.exportSubmission(tid, "project.zip")
				if string(readBytes(download)) != "invalid project archive" {
					t.Fatal("could not export the original blocked download")
				}
				writeFile(str(objects(st["artifacts"])[0], "path"), []byte("unexpected edit"), 0600)
				expectError(t, "冻结版本不一致", func() { a.exportSubmission(tid, "project.zip") })
			}
		})
	}
}
