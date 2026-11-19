package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLiveHarnessWorkflow(t *testing.T) {
	for _, tool := range []string{"codex", "claude"} {
		t.Run(tool, func(t *testing.T) {
			a := testApp(t)
			job := filepath.Join(a.Root, "jobs", "writer")
			exe, err := os.Executable()
			check(err)
			events := []string{
				`{"type":"item.started","item":{"type":"command_execution","command":"go test ./PRIVATE_COMMAND"}}`,
				`{"type":"item.completed","item":{"type":"file_change","changes":[{"path":"final/report.md"}]}}`,
				`{"type":"item.completed","item":{"type":"agent_message","text":"PRIVATE_REASONING"}}`,
			}
			terminal := `{"type":"turn.completed"}`
			if tool == "claude" {
				events = []string{
					`{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"PRIVATE_REASONING"},{"type":"tool_use","name":"Bash","input":{"command":"go test ./PRIVATE_COMMAND"}}]}}`,
					`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Write","input":{"file_path":"final/report.md","content":"PRIVATE_SOURCE"}}]}}`,
					`{"type":"user","message":{"content":[{"type":"tool_result","content":"PRIVATE_RESULT"}]}}`,
				}
				terminal = `{"type":"result","subtype":"success","structured_output":{"ready":true}}`
			}
			writeJSON(filepath.Join(job, "cli-fixture.json"), M{"events": strings.Join(events, "\n") + "\n", "after_release": terminal + "\n", "result": tool == "codex"})
			t.Setenv("CODEX_ENGINE_TEST_CLI", "1")
			plan := M{"writer": tool, "reviewer": tool, tool + "_cli": exe, "stage_timeout": 30}
			st := M{"status": "working", "job": job, "revision": 2, "review_round": 1, "execution_plan": plan}
			done := make(chan error, 1)
			go func() {
				done <- attempt(func() {
					value := a.engine(tool, job, "PRIVATE_PROMPT", writerSchema, "writer", plan)
					ensure(boolean(value, "ready"), "structured result missing")
				})
			}()
			defer func() { writeFile(filepath.Join(job, "release-fixture"), []byte("release"), 0600) }()
			found := false
			for deadline := time.Now().Add(8 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
				rows := a.workspaceTimeline(st, nil)
				encoded := string(jsonBytes(rows))
				if strings.Contains(encoded, "PRIVATE") || strings.Contains(encoded, a.Root) {
					t.Fatal("raw event or job path reached UI", rows)
				}
				if strings.Contains(encoded, "report.md") {
					if !strings.Contains(encoded, "运行测试") || !strings.Contains(encoded, "编辑文件") || str(rows[0], "state") != "active" || str(rows[0], "at") == "" {
						t.Fatal(rows)
					}
					found = true
					break
				}
			}
			writeFile(filepath.Join(job, "release-fixture"), []byte("release"), 0600)
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			if !found {
				t.Fatal("could not see activity before CLI exited")
			}
			if tool == "claude" {
				raw := string(readBytes(filepath.Join(job, "claude.json")))
				if !strings.Contains(raw, "structured_output") || strings.Contains(raw, "PRIVATE") {
					t.Fatal("incorrect persisted terminal result", raw)
				}
			}
			st["status"] = "awaiting"
			st["review_history"] = []M{{"revision": 2, "round": 1, "writer": tool, "reviewer": "codex", "writer_job": job, "reviewed_at": stamp(), "approved": true, "artifact_hashes": M{"final/report.md": "PRIVATE_HASH"}}}
			after := a.workspaceTimeline(st, nil)
			raw := string(jsonBytes(after))
			if strings.Contains(raw, "PRIVATE") || !strings.Contains(raw, "独立复审通过") || !strings.Contains(raw, "运行测试") || strings.Contains(raw, `"state":"active"`) {
				t.Fatal(after)
			}
		})
	}
}

func TestWorkflowUsesReceiptsAndKeepsMissingRunsReadable(t *testing.T) {
	a := testApp(t)
	st := M{"revision": 3, "review_round": 1, "status": "reviewing", "job": filepath.Join(a.Root, "writer"), "review_attempt": M{"job": filepath.Join(a.Root, "reviewer")}, "execution_plan": M{"writer": "codex", "reviewer": "claude"}}
	writeJSON(filepath.Join(str(st, "job"), "complete.json"), M{"hashes": M{"final/code.zip": "hidden", "review.md": "private"}, "finished_at": stamp()})
	rows := a.workspaceTimeline(st, nil)
	if len(rows) != 2 || str(rows[0], "title") != "候选产物已生成" || strings.Join(texts(rows[0]["files"]), ",") != "code.zip" || str(rows[1], "state") != "active" || str(rows[1], "harness") != "claude" {
		t.Fatal(rows)
	}
	writeFile(filepath.Join(str(st, "job"), "process.json"), []byte("truncated"), 0600)
	st["status"] = "failed"
	st["resume_status"] = "reviewing"
	rows = a.workspaceTimeline(st, nil)
	if str(rows[len(rows)-1], "state") != "failed" || strings.Contains(string(jsonBytes(rows)), `"state":"active"`) {
		t.Fatal(rows)
	}
	st["status"] = "revision_ready"
	rows = a.workspaceTimeline(st, nil)
	if number(rows[len(rows)-1], "revision", 0) != 4 {
		t.Fatal(rows)
	}
}

func TestWorkflowClassifiesOnlyRecognizedCommands(t *testing.T) {
	for command, want := range map[string]string{
		"/bin/zsh -lc 'rg -n answer input/'": "search",
		"/bin/bash -c 'go test ./...'":       "test",
		"cat input/course.md":                "read", "ls -l final": "list", "go vet ./...": "check",
		"npm run build": "build", "go build ./cmd/...": "build", "npm test": "test",
		"echo go test": "command", "go test && cat private": "command",
		"/bin/sh -lc 'cat source | other'": "command", "go test $(echo value)": "command",
		"unknown-tool private": "command", "": "command",
	} {
		if got := commandActivity(command); got != want {
			t.Errorf("classification of %q = %q, want %q", command, got, want)
		}
	}
}

func TestTimelineShowsBoundChangesAndTheirReviewedCandidates(t *testing.T) {
	a := testApp(t)
	st := frozen(t, a)
	tid := str(st, "task_id")
	path := "/api/workspace/task/" + tid + "/activity"
	workflow := func() []M {
		return objects(editorResult(t, editorCall(t, a, path, nil))["timeline"])
	}
	before := string(jsonBytes(workflow()))
	a.sendConversation(tid, conversationRequest{ID: strings.Repeat("a", 32), Revision: 1, Kind: "ask", Instruction: "PRIVATE_QUESTION"})
	if got := string(jsonBytes(workflow())); got != before {
		t.Fatal("asking a question changed the workflow", got)
	}
	file := filepath.Base(str(st, "submission"))
	a.sendConversation(tid, conversationRequest{ID: strings.Repeat("b", 32), Revision: 1, Kind: "change", Files: []string{file}, FileRevision: 1, Instruction: "PRIVATE_CHANGE"})
	pending := workflow()
	received := pending[len(pending)-2]
	if str(received, "title") != "已收到修改要求" || number(received, "revision", 0) != 2 || !strings.Contains(str(received, "detail"), "关联第 1 版文件") ||
		strings.Join(texts(received["files"]), ",") != file || str(received, "at") == "" || str(pending[len(pending)-1], "title") != "新版修改已排队" {
		t.Fatal(pending)
	}
	a.RunModel = func(_, job, _ string, _ M, role string, _ M) M {
		if role == "reviewer" {
			return completedReview(true)
		}
		out := resultAt(job, "new bytes")
		out["owner_reply"] = "PRIVATE_REPLY"
		return out
	}
	a.process(readMap(a.taskPath(tid)))
	rows := workflow()
	change, candidate, review, linked := -1, -1, -1, 0
	for i, row := range rows {
		if strings.Contains(str(row, "detail"), "候选产物关联了") {
			linked++
			if number(row, "revision", 0) != 2 || str(row, "role") != "reviewer" {
				t.Fatal("unrelated review linked", row)
			}
		}
		if number(row, "revision", 0) != 2 {
			continue
		}
		switch str(row, "title") {
		case "已收到修改要求":
			if change >= 0 || number(row, "round", 0) != 1 {
				t.Fatal("wrong round or duplicate owner receipt", rows)
			}
			change = i
		case "主写已提供候选产物":
			candidate = i
		case "独立复审通过":
			review = i
		}
	}
	if change < 0 || change >= candidate || candidate >= review || linked != 1 || strings.Contains(string(jsonBytes(rows)), "PRIVATE") {
		t.Fatal(rows)
	}
}
