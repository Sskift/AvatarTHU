package app

import (
	"path/filepath"
	"testing"
)

func TestIgnoredAssignmentSurvivesMaterialUpdatesAndRejectsOldCard(t *testing.T) {
	a := testApp(t)
	a.saveConfig(M{"lark_enabled": true, "lark_user_id": "owner"})
	st := frozen(t, a)
	tid := str(st, "task_id")
	event := ownerEvent(st)
	before := fingerprint(st)
	a.ignoreTask(tid, "无需补交")
	ignored := readMap(a.taskPath(tid))
	if str(ignored, "status") != "ignored" || str(ignored, "ignored_reason") != "无需补交" || str(ignored, "ignored_from_status") != "awaiting" {
		t.Fatal("ignore decision was not saved", ignored)
	}
	// Material changes used to turn every non-submitted task back into a revision.
	writeFile(filepath.Join(str(st, "folder"), "question.txt"), []byte("changed question"), 0600)
	a.syncAssignment(tid, st)
	a.RunModel = func(string, string, string, M, string, M) M { t.Fatal("processed ignored task"); return nil }
	a.SendCard = func(M, string) M { t.Fatal("sent ignored task notification"); return nil }
	a.Upload = func(*School, M, string) M { t.Fatal("uploaded ignored task"); return nil }
	a.process(readMap(a.taskPath(tid)))
	requestPath := filepath.Join(a.editorDir(st), "requests", "queued-rewrite", "request.json")
	writeJSON(requestPath, M{"kind": "rewrite", "state": "queued"})
	a.processEditorRequests(tid)
	if str(readMap(requestPath), "state") != "queued" {
		t.Fatal("processed an editor request for an ignored task")
	}
	if a.act(event) {
		t.Fatal("accepted old submission card")
	}
	a.ignoreTask(tid, "再次确认")
	actual := readMap(a.taskPath(tid))
	if str(actual, "status") != "ignored" || str(actual, "ignored_reason") != "无需补交" || str(actual, "ignored_at") != str(ignored, "ignored_at") {
		t.Fatal("ignore decision changed", actual)
	}
	if fingerprint(actual) != fingerprint(readMap(assignmentPath(str(st, "assignment_dir"), "state.json"))) {
		t.Fatal("assignment state and task state differ")
	}
	for _, key := range []string{"status", "updated_at", "ignored_at", "ignored_reason", "ignored_from_status"} {
		delete(actual, key)
	}
	actual["status"], actual["updated_at"] = st["status"], st["updated_at"]
	if fingerprint(actual) != before {
		t.Fatal("ignore changed historical task data")
	}
}

func TestIgnorePreservesSubmissionStates(t *testing.T) {
	for _, status := range []string{"submitted", "submitting", "submission_unknown"} {
		t.Run(status, func(t *testing.T) {
			a := testApp(t)
			st := fixture(t, a)
			st["status"] = status
			a.saveTask(st)
			before := digest(a.taskPath(str(st, "task_id")))
			expectError(t, "不能改为忽略", func() { a.ignoreTask(str(st, "task_id"), "无需补交") })
			if digest(a.taskPath(str(st, "task_id"))) != before {
				t.Fatal("changed submission record")
			}
		})
	}
}
