package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStudentInputsPauseAfterReviewAndResumeWithFeedback(t *testing.T) {
	for _, approved := range []bool{false, true} {
		name := "rejected"
		if approved {
			name = "approved_with_missing_input"
		}
		t.Run(name, func(t *testing.T) {
			a := testApp(t)
			st := fixture(t, a)
			writers, reviews := 0, 0
			const input = "请提供本人原始录音及实际设备间距。"
			const comment = "PRIVATE REVIEW: analyze the recording"
			a.RunModel = func(_, job, prompt string, _ M, role string, _ M) M {
				if role == "writer" {
					writers++
					r := resultAt(job, "partial generation")
					if writers == 1 {
						// An explicit missing input must override inconsistent ready=true.
						r["student_inputs"] = []string{input}
						return r
					}
					if !exists(filepath.Join(job, "input", "attachments", "recording.txt")) || !exists(filepath.Join(job, "previous-final", "答案.txt")) {
						t.Fatal("resumed writer did not receive new input and previous deliverables")
					}
					if strings.Contains(prompt, comment) == approved || !strings.Contains(prompt, "Recording now supplied") {
						t.Fatal("resumed writer lost owner feedback or latest rejected review")
					}
					r["student_inputs"] = []string{}
					return r
				}
				reviews++
				for _, p := range visibleFiles(job) {
					b, err := os.ReadFile(p)
					check(err)
					if strings.Contains(string(b), comment) || strings.Contains(string(b), "PRIVATE SELF ASSESSMENT") {
						t.Fatal("reviewer received earlier review or writer self assessment")
					}
				}
				return M{"approved": approved || reviews == 2, "summary": "independent result", "comments": []M{{"location": "recording", "comment": comment, "suggestion": "use the original capture"}}, "checks": []string{}, "limitations": []string{}}
			}
			a.process(st)
			if writers != 1 || reviews != 1 || str(st, "status") != "needs_student" || boolean(st, "ready") {
				t.Fatalf("missing input reran or became submittable: writers=%d reviews=%d status=%v", writers, reviews, st["status"])
			}
			if len(texts(st["student_inputs"])) != 1 || len(texts(st["blockers"])) != 1 || len(objects(st["review_history"])) != 1 {
				t.Fatal("lost input requirement or independent review")
			}
			page, err := os.ReadFile(str(st, "local_review"))
			check(err)
			if !strings.Contains(string(page), input) || !strings.Contains(string(page), comment) {
				t.Fatal("published review omitted input requirement or review comments")
			}
			a.process(st)
			if writers != 1 || reviews != 1 {
				t.Fatal("waiting for student restarted model work")
			}
			writeFile(filepath.Join(str(st, "folder"), "recording.txt"), []byte("new input"), 0600)
			merge(st, M{"status": "revision_ready", "feedback": "Recording now supplied"})
			a.process(st)
			if writers != 2 || reviews != 2 || str(st, "status") != "awaiting" || number(st, "revision", 0) != 2 {
				t.Fatal("resumed assignment did not finish with a fresh review")
			}
			if len(texts(st["student_inputs"])) != 0 || len(texts(st["blockers"])) != 0 || len(objects(st["review_history"])) != 2 {
				t.Fatal("resume retained resolved inputs or lost review history")
			}
		})
	}
}

func TestOrdinaryWriterBlockersStillRewrite(t *testing.T) {
	a := testApp(t)
	st := fixture(t, a)
	writers, reviews := 0, 0
	a.RunModel = func(_, job, prompt string, _ M, role string, _ M) M {
		if role == "writer" {
			writers++
			r := resultAt(job, "answer")
			if writers == 1 {
				r["ready"] = false
				r["blockers"] = []string{"calculation needs repair"}
			} else if !strings.Contains(prompt, "fix calculation") {
				t.Fatal("ordinary rejection did not return to writer")
			}
			return r
		}
		reviews++
		return M{"approved": reviews == 2, "summary": "fix calculation", "comments": []M{}, "checks": []string{}, "limitations": []string{}}
	}
	a.process(st)
	if writers != 2 || reviews != 2 || str(st, "status") != "awaiting" {
		t.Fatal("repairable blocker stopped automatic rewriting")
	}
}

func TestStudentInputsValidationAndCheckpointCompatibility(t *testing.T) {
	a := testApp(t)
	job := filepath.Join(t.TempDir(), "writer")
	r := resultAt(job, "partial")
	validateWriter(r, job) // Legacy results have no student_inputs field.
	if !boolean(r, "ready") {
		t.Fatal("legacy result changed")
	}
	for _, invalid := range []any{"not a list", nil, []string{" "}, []any{1}} {
		r["student_inputs"] = invalid
		if attempt(func() { validateWriter(r, job) }) == nil {
			t.Fatalf("accepted invalid student inputs: %v", invalid)
		}
	}
	r["student_inputs"] = []string{"original recording"}
	r = validateWriter(r, job)
	writeJSON(filepath.Join(job, "result.json"), r)
	writeJSON(filepath.Join(job, "complete.json"), M{"hashes": hashesFor(job, append(texts(r["files"]), "review.md"))})
	a.RunModel = func(string, string, string, M, string, M) M {
		t.Fatal("checkpoint invoked a model")
		return nil
	}
	resumed := a.writer(job, "", a.pairing())
	if len(texts(resumed["blockers"])) != 1 || boolean(resumed, "ready") {
		t.Fatal("checkpoint duplicated or lost missing input")
	}
}
