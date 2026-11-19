package app

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestOwnerAdjustsRunningWriterAndReviewer(t *testing.T) {
	a := testApp(t)
	st := fixture(t, a)
	a.saveTask(st)
	tid := str(st, "task_id")
	path := "/api/workspace/task/" + tid + "/conversation"
	send := func(id, text string) {
		t.Helper()
		editorResult(t, editorCall(t, a, path, M{"id": strings.Repeat(id, 32), "kind": "change", "revision": 1, "instruction": text}))
	}
	writer, reviewer := 0, 0
	a.RunModel = func(tool, job, prompt string, _ M, role string, _ M) M {
		if role == "reviewer" {
			reviewer++
			if strings.Contains(prompt, "OWNER_") || strings.Contains(prompt, "REPLY_") {
				t.Fatal("writer conversation went into reviewer prompt")
			}
			for _, file := range visibleFiles(filepath.Join(job, "input")) {
				body := string(readBytes(file))
				if strings.Contains(body, "OWNER_") || strings.Contains(body, "REPLY_") {
					t.Fatal("conversation leaked into review materials")
				}
			}
			content := string(readBytes(filepath.Join(job, "submission", "答案.txt")))
			if content != fmt.Sprintf("artifact %d", reviewer+1) {
				t.Fatal("reviewed superseded writer candidate", content)
			}
			if reviewer == 1 {
				send("c", "OWNER_C")
			}
			return completedReview(true)
		}
		writer++
		if writer == 1 {
			send("b", "OWNER_B")
		}
		for i, key := range []string{"OWNER_A", "OWNER_B", "OWNER_C"} {
			if i < writer && !strings.Contains(prompt, key) {
				t.Fatal("lost owner context", writer, key)
			}
		}
		if writer >= 2 {
			if prev := string(readBytes(filepath.Join(job, "previous-final", "答案.txt"))); prev != fmt.Sprintf("artifact %d", writer-1) {
				t.Fatal("writer did not receive most recent candidate", prev)
			}
			if !strings.Contains(prompt, "REPLY_1") {
				t.Fatal("second writer lost prior reply")
			}
		}
		result := resultAt(job, fmt.Sprintf("artifact %d", writer))
		result["owner_reply"] = fmt.Sprintf("REPLY_%d", writer)
		return result
	}
	send("a", "OWNER_A")
	func() {
		defer a.lock(tid, true)()
		a.process(st)
	}()
	if writer != 3 || reviewer != 2 || number(st, "revision", 0) != 1 || len(objects(st["review_history"])) != 2 || str(st, "status") != "awaiting" {
		t.Fatal("unexpected pipeline after live adjustments", writer, reviewer, st["status"])
	}
	rows := objects(a.conversationView(st)["messages"])
	if len(rows) != 6 {
		t.Fatal(rows)
	}
	for _, m := range rows {
		if str(m, "state") != "answered" || m["plan"] != nil || m["job"] != nil {
			t.Fatal("public chat exposes internals or lost message", m)
		}
	}
	if len(texts(rows[len(rows)-1]["reply_to"])) != 1 || str(rows[len(rows)-1], "text") != "REPLY_3" {
		t.Fatal(rows)
	}
	// Repeated delivery of the POST after publication must not start another revision.
	send("a", "OWNER_A")
	if str(readMap(a.taskPath(tid)), "status") != "awaiting" || len(objects(a.conversationView(st)["messages"])) != 6 {
		t.Fatal("retry duplicated a turn")
	}
}

func TestConversationPinsFilesAndThawsOnlyNewRevision(t *testing.T) {
	a := testApp(t)
	st := frozen(t, a)
	tid := str(st, "task_id")
	old := str(st, "submission")
	oldHash := digest(old)
	name := filepath.Base(old)
	req := conversationRequest{ID: strings.Repeat("a", 32), Kind: "change", Revision: 1, Instruction: "OWNER_SCOPED", Files: []string{name}, FileRevision: 2}
	expectError(t, "版本已更新", func() { a.sendConversation(tid, req) })
	req.FileRevision = 1
	a.sendConversation(tid, req)
	st = readMap(a.taskPath(tid))
	if str(st, "status") != "revision_ready" {
		t.Fatal("old current card remained actionable")
	}
	saved := objects(a.conversationView(st)["messages"])[0]
	if number(saved, "target_revision", 0) != 2 || number(saved, "file_revision", 0) != 1 {
		t.Fatal(saved)
	}
	a.RunModel = func(tool, job, prompt string, _ M, role string, _ M) M {
		if role == "reviewer" {
			return completedReview(true)
		}
		if !strings.Contains(prompt, name) || !strings.Contains(prompt, "OWNER_SCOPED") || digest(filepath.Join(job, "previous-final", name)) != oldHash {
			t.Fatal("owner file/version lost")
		}
		return resultAt(job, "new version")
	}
	a.process(st)
	if number(st, "revision", 0) != 2 || digest(old) != oldHash || str(st, "status") != "awaiting" {
		t.Fatal("changed frozen edition", st["status"])
	}
	a.sendConversation(tid, req)
	req.ID = strings.Repeat("b", 32)
	expectError(t, "版本已更新", func() { a.sendConversation(tid, req) })
	req.Revision = 2
	for _, blocked := range []string{"submitted", "submitting", "submission_unknown", "ignored", "closed_remote"} {
		st["status"] = blocked
		a.saveTask(st)
		expectError(t, "不能修改", func() { a.sendConversation(tid, req) })
	}
}

func TestConversationRecoveryAtPublicationCutoff(t *testing.T) {
	a := testApp(t)
	st := frozen(t, a)
	tid := str(st, "task_id")
	st["status"] = "reviewing"
	a.saveTask(st)
	if a.conversationCheckpoint(st, str(st, "job"), true) {
		t.Fatal("unexpected rewrite")
	}
	req := conversationRequest{ID: strings.Repeat("a", 32), Kind: "change", Revision: 1, Instruction: "late request"}
	a.sendConversation(tid, req)
	r := objects(a.conversationView(st)["messages"])[0]
	if number(r, "target_revision", 0) != 2 || str(r, "state") != "queued" {
		t.Fatal(r)
	}
	st["status"] = "awaiting"
	a.saveTask(st)
	// The normal worker and card handler call this under the task lock even
	// if the daemon crashed before queuing the newly requested revision.
	a.queuePendingConversation(st)
	if str(st, "status") != "revision_ready" || !strings.Contains(str(st, "feedback"), "late request") {
		t.Fatal("lost late instruction")
	}
}

func TestConversationKeepsReviewCapAndRecoversBlockedTurns(t *testing.T) {
	a := testApp(t)
	a.saveConfig(M{"lark_enabled": false, "max_review_rounds": 1})
	st := fixture(t, a)
	a.saveTask(st)
	tid := str(st, "task_id")
	req := conversationRequest{ID: strings.Repeat("c", 32), Kind: "change", Revision: 1, Instruction: "apply in new revision"}
	reviews := 0
	a.RunModel = func(_, job, prompt string, _ M, role string, _ M) M {
		if role == "writer" {
			return resultAt(job, "answer")
		}
		reviews++
		if reviews == 1 {
			a.sendConversation(tid, req)
		}
		return completedReview(true)
	}
	a.process(st)
	if str(st, "status") != "revision_ready" || len(objects(st["review_history"])) != 1 {
		t.Fatal("exceeded the per-version review limit", st["status"], reviews)
	}
	row := objects(a.conversationView(st)["messages"])[0]
	if number(row, "target_revision", 0) != 2 {
		t.Fatal(row)
	}
	st["status"] = "failed"
	a.saveTask(st)
	if got := str(objects(a.conversationView(st)["messages"])[0], "state"); got != "waiting" {
		t.Fatal(got)
	}
	st["status"] = "revision_ready"
	a.saveTask(st)
	a.process(st)
	history := objects(st["review_history"])
	if reviews != 2 || len(history) != 2 || number(history[1], "revision", 0) != 2 || number(history[1], "round", 0) != 1 || !slices.Equal(texts(history[1]["owner_message_ids"]), []string{req.ID}) {
		t.Fatal(history)
	}
}

func TestOwnerChangeDuringCardVerification(t *testing.T) {
	a := testApp(t)
	a.saveConfig(M{"lark_enabled": true, "lark_user_id": "owner"})
	st := frozen(t, a)
	tid := str(st, "task_id")
	a.Verify = func(M) *School {
		a.sendConversation(tid, conversationRequest{ID: strings.Repeat("d", 32), Kind: "change", Revision: 1, Instruction: "Adjust before submitting"})
		return nil
	}
	a.Upload = func(*School, M, string) M { t.Fatal("card uploaded after new owner request"); return nil }
	a.SendCard = func(M, string) M { t.Fatal("unexpected receipt"); return nil }
	if a.act(ownerEvent(st)) || str(readMap(a.taskPath(tid)), "status") != "revision_ready" {
		t.Fatal("lost change accepted during card verification")
	}
}
