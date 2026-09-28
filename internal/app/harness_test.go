package app

import (
	"os"
	"os/exec"
	"reflect"
	"testing"
)

func TestHarnessChildHeadlessEnvironment(t *testing.T) {
	if os.Getenv("AVATARTHU_ENV_CHECK") == "1" {
		if os.Getenv("AVATARTHU_HEADLESS") != "1" {
			t.Fatal("background harness did not receive headless mode")
		}
		if os.Getenv("AVATARTHU_TEST_SECRET") != "" || os.Getenv("LARK_TEST_SECRET") != "" || os.Getenv("CLAUDECODE") != "" {
			t.Fatal("private parent environment reached the harness")
		}
		return
	}
	t.Setenv("AVATARTHU_HEADLESS", "0")
	t.Setenv("AVATARTHU_TEST_SECRET", "private")
	t.Setenv("LARK_TEST_SECRET", "private")
	t.Setenv("CLAUDECODE", "1")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^TestHarnessChildHeadlessEnvironment$")
	cmd.Env = append(modelEnv(), "AVATARTHU_ENV_CHECK=1")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("harness environment: %v\n%s", err, output)
	}
}

func TestConfigureHarnessesPreservesLegacyAndOtherRole(t *testing.T) {
	a := testApp(t)
	a.saveConfig(M{"review_mode": "codex-claude", "poll_interval_seconds": 7200, "lark_profile": "existing"})
	a.configure([]string{"--poll-interval", "6h"})
	if p := a.pairing(); str(p, "writer") != "codex" || str(p, "reviewer") != "claude" {
		t.Fatal("legacy roles changed", p)
	}
	a.configure([]string{"--reviewer", "codex"})
	if p := a.pairing(); str(p, "mode") != "codex-codex" {
		t.Fatal(p)
	}
	a.configure([]string{"--writer", "claude"})
	if p := a.pairing(); str(p, "mode") != "claude-codex" {
		t.Fatal(p)
	}
	a.configure([]string{"--mode", "codex-claude"})
	if p := a.pairing(); str(p, "mode") != "codex-claude" {
		t.Fatal(p)
	}
	cfg := a.config()
	if str(cfg, "lark_profile") != "existing" || number(cfg, "poll_interval_seconds", 0) != 21600 {
		t.Fatal("unrelated configuration changed", cfg)
	}
	for _, args := range [][]string{{"--writer", "gemini"}, {"--reviewer", ""}, {"--mode", "codex-claude", "--writer", "claude"}, {"--mode", "invalid"}} {
		if e := attempt(func() { a.configure(args) }); e == nil {
			t.Fatalf("accepted invalid options %v", args)
		}
		if !reflect.DeepEqual(cfg, a.config()) {
			t.Fatal("invalid options changed saved configuration")
		}
	}
}

func TestOnlySelectedHarnessFailureBlocksVersion(t *testing.T) {
	a := testApp(t)
	a.recordToolFailure(diagnosticError("codex", "quota exceeded", ""), "reviewer", "")
	a.saveConfig(M{"writer_harness": "claude", "reviewer_harness": "claude"})
	a.requireUnblockedTools(a.pairing())
	// A version still using Codex must retain its original failure, even when
	// the user's default for future versions now uses only Claude.
	expectError(t, "额度", func() { a.requireUnblockedTools(M{"writer": "claude", "reviewer": "codex"}) })
	if str(readMap(a.data("codex-execution.json")), "state") != "failed" {
		t.Fatal("lost unselected harness failure")
	}
}
