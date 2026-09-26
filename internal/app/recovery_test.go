package app

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFailuresRequireAppropriateRecovery(t *testing.T) {
	for _, item := range []struct {
		message, category string
		retry             bool
	}{
		{"401 unauthorized", "auth", false}, {"429 quota exceeded", "quota", false},
		{"unknown option", "version", false}, {"403 forbidden", "permission", false},
		{"connection timeout", "network", true}, {"model not found", "configuration", false},
	} {
		t.Run(item.category, func(t *testing.T) {
			a := testApp(t)
			st := fixture(t, a)
			st["status"] = "reviewing"
			a.saveTaskFailure(st, diagnosticError("codex", item.message, ""))
			st["retry_at"] = time.Now().Add(-time.Hour).Format(time.RFC3339)
			if taskRetryDue(st, true) != item.retry {
				t.Fatal("wrong retry policy")
			}
			if str(obj(st, "failure"), "category") != item.category || str(st, "resume_status") != "reviewing" {
				t.Fatal(st)
			}
		})
	}
}

func TestRetryPreservesUnknownSubmissionsAndExecutionFailure(t *testing.T) {
	a := testApp(t)
	calls := 0
	a.RunModel = func(string, string, string, M, string, M) M { calls++; return M{} }
	a.Upload = func(*School, M, string) M { t.Fatal("retry uploaded homework"); return nil }
	err := diagnosticError("codex", "quota exceeded", "")
	a.recordToolFailure(err, "reviewer", "")
	failed := fixture(t, a)
	failed["status"] = "reviewing"
	a.saveTaskFailure(failed, err)
	unknown := M{"task_id": "1123456789abcdef", "status": "submission_unknown", "failure": failureFields(err), "sha256": "unchanged", "nonce": "original"}
	a.saveTask(unknown)
	expectError(t, "额度", func() { a.requireUnblockedTools(a.pairing()) })
	if a.retryTool("codex") != 1 {
		t.Fatal("wrong number queued")
	}
	a.requireUnblockedTools(a.pairing())
	if calls != 0 {
		t.Fatal("retry launched a model")
	}
	h := readMap(a.data("codex-execution.json"))
	if str(h, "state") != "failed" || str(h, "retry_requested_at") == "" {
		t.Fatal("cleared actual execution failure during login check")
	}
	if !taskRetryDue(readMap(a.taskPath(str(failed, "task_id"))), true) {
		t.Fatal("failed task not resumed")
	}
	saved := readMap(a.taskPath(str(unknown, "task_id")))
	if str(saved, "status") != "submission_unknown" || str(saved, "nonce") != "original" {
		t.Fatal("changed unknown submission")
	}
}

func TestExecutionFailureClearedOnlyBySuccessfulExecution(t *testing.T) {
	a := testApp(t)
	job := filepath.Join(a.Root, "job")
	a.RunModel = func(string, string, string, M, string, M) M { panic(diagnosticError("claude", "quota exceeded", "")) }
	expectError(t, "额度", func() { a.engine("claude", job, "", writerSchema, "writer", M{}) })
	h := readMap(a.data("claude-execution.json"))
	if str(h, "category") != "quota" || str(h, "phase") != "writer" {
		t.Fatal(h)
	}
	a.RunModel = func(string, string, string, M, string, M) M { return M{"ready": true} }
	a.engine("claude", job, "", writerSchema, "writer", M{})
	if str(readMap(a.data("claude-execution.json")), "state") != "succeeded" {
		t.Fatal("execution failure was not cleared")
	}
	if strings.Contains(reviewerPrompt(job), "revision_notes") {
		t.Fatal("writer revision claims sent to reviewer")
	}
}
