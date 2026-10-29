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

func TestRestoreIgnoredTaskPreservesVersionAndHistory(t *testing.T) {
	a := testApp(t)
	st := frozen(t, a)
	st["source_hash"] = materialsHash(st)
	a.saveTask(st)
	tid := str(st, "task_id")
	for _, reason := range []string{"暂不需要", "再次忽略"} {
		a.ignoreTask(tid, reason)
		a.restoreTask(tid)
		restored := readMap(a.taskPath(tid))
		if str(restored, "status") != "awaiting" || str(restored, "ignored_at") != "" || str(restored, "ignored_reason") != "" {
			t.Fatal("did not restore original status", restored)
		}
		for _, key := range []string{"revision", "artifacts", "submission", "sha256", "card_message_id", "nonce", "review_history"} {
			if fingerprint(restored[key]) != fingerprint(st[key]) {
				t.Fatal("restore changed version data", key)
			}
		}
		history := objects(restored["ignore_history"])
		if str(history[len(history)-1], "reason") != reason || str(history[len(history)-1], "restored_at") == "" {
			t.Fatal("missing ignore history", history)
		}
		before := digest(a.taskPath(tid))
		a.restoreTask(tid)
		if digest(a.taskPath(tid)) != before {
			t.Fatal("duplicate restore changed state")
		}
	}
	if len(objects(readMap(a.taskPath(tid))["ignore_history"])) != 2 {
		t.Fatal("lost previous ignore decision")
	}
	if digest(str(st, "submission")) != str(st, "sha256") {
		t.Fatal("restore changed frozen submission")
	}
}

func TestIgnoreWaitsForActiveEditorRequest(t *testing.T) {
	a, st := editorFixture(t)
	tid := str(st, "task_id")
	path := filepath.Join(a.editorDir(st), "requests", "active-rewrite", "request.json")
	writeJSON(path, M{"kind": "rewrite", "state": "running"})
	before := digest(a.taskPath(tid))
	expectError(t, "局部修改正在处理", func() { a.ignoreTask(tid, "无需补交") })
	if digest(a.taskPath(tid)) != before {
		t.Fatal("ignored a task with an active editor request")
	}
	writeJSON(path, M{"kind": "rewrite", "state": "ready"})
	a.ignoreTask(tid, "无需补交")
	if str(readMap(a.taskPath(tid)), "status") != "ignored" {
		t.Fatal("could not ignore after editing completed")
	}
}

func TestRestoreRechecksMaterialsChangedWhileIgnored(t *testing.T) {
	for _, change := range []string{"attachment", "description", "cached"} {
		t.Run(change, func(t *testing.T) {
			a := testApp(t)
			st := frozen(t, a)
			st["source_hash"] = materialsHash(st)
			st["source_cached"] = change == "cached"
			a.saveTask(st)
			tid := str(st, "task_id")
			a.ignoreTask(tid, "暂不需要")
			meta := M{}
			for _, key := range []string{"folder", "courseware", "description", "deadline", "requirements_hash"} {
				meta[key] = st[key]
			}
			if change == "attachment" {
				writeFile(filepath.Join(str(st, "folder"), "question.txt"), []byte("2+2?"), 0600)
			} else if change == "description" {
				meta["description"] = "更新后的作业要求"
			}
			writeJSON(assignmentPath(str(st, "assignment_dir"), "assignment.json"), meta)
			a.syncAssignment(tid, meta)
			a.restoreTask(tid)
			restored := readMap(a.taskPath(tid))
			if str(restored, "status") != "revision_ready" || str(restored, "description") != str(meta, "description") || str(restored, "source_hash") != materialsHash(meta) || boolean(restored, "source_cached") {
				t.Fatal("reactivated a stale version", restored)
			}
			if str(restored, "card_message_id") != str(st, "card_message_id") || digest(str(st, "submission")) != str(st, "sha256") {
				t.Fatal("restore lost historical card or artifact")
			}
		})
	}
}

func TestRestoreCannotEnterSubmissionStates(t *testing.T) {
	for _, status := range []string{"submitted", "submitting", "submission_unknown", "ignored", ""} {
		t.Run(status, func(t *testing.T) {
			a := testApp(t)
			st := fixture(t, a)
			merge(st, M{"status": "ignored", "ignored_from_status": status})
			a.saveTask(st)
			tid := str(st, "task_id")
			before := digest(a.taskPath(tid))
			expectError(t, "忽略前的状态无效", func() { a.restoreTask(tid) })
			if digest(a.taskPath(tid)) != before {
				t.Fatal("invalid restore changed state")
			}
		})
	}
}
