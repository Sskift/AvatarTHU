package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestQuestionUsesFrozenFilesAlongsideActiveWriter(t *testing.T) {
	a := testApp(t)
	st := frozen(t, a)
	tid := str(st, "task_id")
	// The delivery is still available while a new writer holds the task and
	// model locks. A question must neither wait for them nor modify this state.
	st["status"] = "working"
	st["revision"] = 2
	a.saveTask(st)
	path := a.taskPath(tid)
	before := digest(path)
	health := a.data("codex-execution.json")
	writeJSON(health, M{"state": "running", "job": "unrelated-live-writer"})
	healthHash := digest(health)
	unlockTask := a.lock(tid, false)
	defer unlockTask()
	unlockWriter := a.lock("model-worker", false)
	defer unlockWriter()
	apiPath := "/api/workspace/task/" + tid + "/conversation"
	name := filepath.Base(str(st, "submission"))
	id := strings.Repeat("a", 32)
	send := func(id, text string) {
		editorResult(t, editorCall(t, a, apiPath, M{"id": id, "kind": "ask", "revision": 2, "file_revision": 1, "instruction": text, "files": []string{name}}))
	}
	send(id, "why is this 2?")
	send(id, "why is this 2?") // A lost HTTP acknowledgement must not start a second session.
	send(strings.Repeat("b", 32), "what did you check?")
	calls := 0
	a.RunModel = func(executor, job, prompt string, _ M, role string, plan M) M {
		calls++
		if role != "question" || !boolean(plan, "read_only") || string(readBytes(filepath.Join(job, "files", name))) != "2" ||
			!exists(filepath.Join(job, "materials", "assignment.md")) {
			t.Fatalf("wrong question context: %s", job)
		}
		if calls == 1 {
			if strings.Contains(prompt, "what did you check?") {
				t.Fatal("a later turn entered the earlier question")
			}
			return M{"answer": "FIRST_ANSWER"}
		}
		if !strings.Contains(prompt, "FIRST_ANSWER") {
			t.Fatal("follow-up lost the preceding reply")
		}
		return M{"answer": "SECOND_ANSWER"}
	}
	a.processConversationQuestions(tid)
	a.processConversationQuestions(tid)
	view := a.conversationView(st)
	if calls != 2 || len(objects(view["messages"])) != 4 || !strings.Contains(string(jsonBytes(view)), "SECOND_ANSWER") ||
		digest(path) != before || digest(health) != healthHash {
		t.Fatal("question changed a task or did not return one reply per turn", calls, view)
	}
}

func TestQuestionCommandsAreReadOnly(t *testing.T) {
	job := t.TempDir()
	for _, tool := range []string{"codex", "claude"} {
		executable, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		plan := M{tool + "_cli": executable, "read_only": true}
		args := modelCommand(tool, job, questionSchema, plan)
		get := func(flag string) string {
			for i, v := range args {
				if v == flag && i+1 < len(args) {
					return args[i+1]
				}
			}
			return ""
		}
		if tool == "codex" && get("--sandbox") != "read-only" || tool == "claude" && get("--tools") != "Read,Glob,Grep" {
			t.Fatal("question gained editing tools", tool)
		}
	}
}
