package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runCodexEngineTestCLI() {
	var job, output string
	for i, arg := range os.Args[:len(os.Args)-1] {
		switch arg {
		case "-C":
			job = os.Args[i+1]
		case "--output-last-message":
			output = os.Args[i+1]
		}
	}
	fixture := readMap(filepath.Join(job, "cli-fixture.json"))
	if boolean(fixture, "result") {
		writeJSON(output, M{"ready": true, "approved": true})
	}
	fmt.Fprint(os.Stdout, str(fixture, "events"))
	fmt.Fprint(os.Stderr, str(fixture, "stderr"))
	os.Exit(number(fixture, "exit_code", 0))
}

func TestCodexEngineRecoveredErrorsAndTerminalFailures(t *testing.T) {
	completed := `{"type":"turn.completed"}`
	reconnecting := `{"type":"error","message":"Reconnecting... 2/5 (stream disconnected before completion: tls handshake eof)"}`
	failed := `{"type":"turn.failed","error":{"message":"stream disconnected before completion"}}`
	for _, tc := range []struct {
		name     string
		role     string
		events   []string
		result   bool
		exitCode int
		wantOK   bool
	}{
		{"success", "writer", []string{completed}, true, 0, true},
		{"writer recovers connection", "writer", []string{reconnecting, completed}, true, 0, true},
		{"reviewer recovers repeated connections", "reviewer", []string{reconnecting, reconnecting, completed}, true, 0, true},
		{"terminal failure", "writer", []string{reconnecting, failed}, true, 0, false},
		{"terminal failure is not cleared", "writer", []string{failed, completed}, true, 0, false},
		{"missing completion", "writer", []string{reconnecting}, true, 0, false},
		{"unfinished next turn", "writer", []string{completed, `{"type":"turn.started"}`}, true, 0, false},
		{"error after completion", "writer", []string{completed, reconnecting}, true, 0, false},
		{"nonzero exit", "writer", []string{reconnecting, completed}, true, 1, false},
		{"missing result", "writer", []string{reconnecting, completed}, false, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := testApp(t)
			job := filepath.Join(a.Root, "job")
			writeJSON(filepath.Join(job, "cli-fixture.json"), M{
				"events": strings.Join(tc.events, "\n") + "\n", "result": tc.result,
				"exit_code": tc.exitCode, "stderr": "failed to connect to websocket: tls handshake eof\n",
			})
			exe, err := os.Executable()
			check(err)
			t.Setenv("CODEX_ENGINE_TEST_CLI", "1")
			schema := writerSchema
			if tc.role == "reviewer" {
				schema = reviewerSchema
			}
			err = attempt(func() {
				a.engine("codex", job, "fixture", schema, tc.role, M{"codex_cli": exe, "stage_timeout": 30})
			})
			if (err == nil) != tc.wantOK {
				t.Fatalf("success = %v, want %v: %v", err == nil, tc.wantOK, err)
			}
			receipt := readMap(filepath.Join(job, "execution.json"))
			if (str(receipt, "completed_at") != "") != tc.wantOK {
				t.Fatal("execution receipt disagrees with terminal outcome", receipt)
			}
			health := readMap(a.data("codex-execution.json"))
			if (str(health, "state") == "succeeded") != tc.wantOK {
				t.Fatal("tool health disagrees with terminal outcome", health)
			}
		})
	}
}
