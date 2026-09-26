package app

import (
	"reflect"
	"testing"
)

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
