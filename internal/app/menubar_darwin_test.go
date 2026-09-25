package app

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestMenubarBundleUpgradeAndRollback(t *testing.T) {
	root := t.TempDir()
	target, staging := filepath.Join(root, "AvatarTHU.app"), filepath.Join(root, "new.app")
	writeFile(filepath.Join(target, "Contents", "version"), []byte("old"), 0600)
	writeFile(filepath.Join(staging, "Contents", "version"), []byte("new"), 0600)
	if e := replaceMenubarBundle(staging, target); e != nil {
		t.Fatal(e)
	}
	if e := replaceMenubarBundle(filepath.Join(root, "missing.app"), target); e == nil {
		t.Fatal("missing bundle must fail")
	}
	value, e := os.ReadFile(filepath.Join(target, "Contents", "version"))
	if e != nil || string(value) != "new" {
		t.Fatalf("upgrade or rollback lost the installed app: %s %v", value, e)
	}
}

func TestMenubarSnapshot(t *testing.T) {
	binary := os.Getenv("AVATARTHU_MENUBAR_TEST_BINARY")
	if binary == "" {
		t.Skip("set AVATARTHU_MENUBAR_TEST_BINARY to test the compiled AppKit monitor")
	}
	for _, test := range []struct{ name, want string }{
		{"healthy", "running"}, {"working", "busy"}, {"expired", "attention"},
		{"stale_keepalive", "attention"}, {"stale_pid", "stopped"},
		{"wrong_executable", "stopped"}, {"dead_listener", "attention"},
	} {
		t.Run(test.name, func(t *testing.T) {
			a := testApp(t)
			mkdir(filepath.Join(a.Root, "bin"))
			executable, e := os.Executable()
			check(e)
			if test.name != "wrong_executable" {
				check(os.Symlink(executable, a.installedBinary()))
			}
			health := M{"state": "running", "pid": os.Getpid()}
			keepalive := M{"state": "valid", "last_success": stamp()}
			if test.name == "stale_pid" {
				health["pid"] = 2147483647
			}
			if test.name == "expired" {
				keepalive["state"] = "expired"
			}
			if test.name == "stale_keepalive" {
				keepalive["last_success"] = time.Now().Add(-time.Hour).Format(time.RFC3339)
			}
			if test.name == "working" {
				writeJSON(a.taskPath("0123456789abcdef"), M{"status": "reviewing"})
			}
			if test.name == "dead_listener" {
				a.saveConfig(M{"lark_enabled": true})
				for _, name := range []string{"actions", "messages"} {
					writeJSON(a.data(name+"-health.json"), M{"state": "ready", "pid": 2147483647})
				}
			}
			writeJSON(a.data("daemon-health.json"), health)
			writeJSON(filepath.Join(a.Root, "keepalive-status.json"), keepalive)
			out, e := exec.Command(binary, "--home", a.Root, "--snapshot").CombinedOutput()
			if e != nil {
				t.Fatalf("native monitor failed: %v %s", e, out)
			}
			var result M
			if e := json.Unmarshal(out, &result); e != nil {
				t.Fatal(e)
			}
			if str(result, "state") != test.want {
				t.Fatalf("want %s, got %s", test.want, out)
			}
		})
	}
}

func TestMenubarIndependentIndicators(t *testing.T) {
	binary := os.Getenv("AVATARTHU_MENUBAR_TEST_BINARY")
	if binary == "" {
		t.Skip("set AVATARTHU_MENUBAR_TEST_BINARY to test the compiled AppKit monitor")
	}
	for _, name := range []string{"all_ready", "codex_login_failed", "stale_cli", "missing_cli", "partial_lark", "lark_disabled", "learn_expired", "runtime_quota", "requested_retry", "recovered", "active_mode"} {
		t.Run(name, func(t *testing.T) {
			a := testApp(t)
			mkdir(filepath.Join(a.Root, "bin"))
			executable, e := os.Executable()
			check(e)
			check(os.Symlink(executable, a.installedBinary()))
			a.saveConfig(M{"lark_enabled": name != "lark_disabled"})
			writeJSON(a.data("daemon-health.json"), M{"state": "running", "pid": os.Getpid()})
			for _, listener := range []string{"actions", "messages"} {
				pid := os.Getpid()
				if name == "partial_lark" && listener == "messages" {
					pid = 2147483647
				}
				writeJSON(a.data(listener+"-health.json"), M{"state": "ready", "pid": pid})
			}
			keepalive := M{"state": "valid", "last_success": stamp()}
			if name == "learn_expired" {
				keepalive["state"] = "expired"
			}
			writeJSON(filepath.Join(a.Root, "keepalive-status.json"), keepalive)
			records := []M{{"tool": "claude", "usable": true, "checked_at": stamp()}, {"tool": "codex", "usable": true, "checked_at": stamp()}}
			want := map[string]string{"lark_cli": "ready", "learn": "ready", "claude": "ready", "codex": "ready"}
			switch name {
			case "codex_login_failed":
				merge(records[1], M{"usable": false, "reason": "codex 尚未登录"})
				want["codex"] = "error"
			case "stale_cli":
				records[0]["checked_at"] = time.Now().Add(-time.Hour).Format(time.RFC3339)
				records[1]["checked_at"] = time.Now().Add(time.Hour).Format(time.RFC3339)
				want["claude"], want["codex"] = "stale", "stale"
			case "missing_cli":
				records = records[:1]
				want["codex"] = "unknown"
			case "partial_lark":
				want["lark_cli"] = "unknown"
			case "lark_disabled":
				want["lark_cli"] = "off"
			case "learn_expired":
				want["learn"] = "error"
			case "runtime_quota", "requested_retry":
				health := M{"state": "failed", "reason": "额度或频率限制", "category": "quota", "retryable": false}
				if name == "requested_retry" {
					health["retry_requested_at"] = stamp()
				}
				writeJSON(a.data("claude-execution.json"), health)
				want["claude"] = "error"
			case "recovered":
				writeJSON(a.data("claude-execution.json"), M{"state": "succeeded"})
			case "active_mode":
				a.saveConfig(M{"lark_enabled": true, "review_mode": "codex-claude"})
				writeJSON(a.taskPath("0123456789abcdef"), M{"status": "reviewing", "execution_plan": M{"mode": "claude-codex"}})
			}
			writeJSON(a.data("cli-health.json"), records)
			out, e := exec.Command(binary, "--home", a.Root, "--snapshot").CombinedOutput()
			if e != nil {
				t.Fatalf("native monitor failed: %v %s", e, out)
			}
			var result M
			check(json.Unmarshal(out, &result))
			for tool, state := range want {
				if str(obj(result, "indicators"), tool) != state {
					t.Errorf("%s: want %s, got %s", tool, state, out)
				}
			}
			if name == "active_mode" && (str(result, "review_mode") != "codex-claude" || len(texts(result["active_review_modes"])) != 1 || texts(result["active_review_modes"])[0] != "claude-codex") {
				t.Fatalf("lost running version's pairing: %s", out)
			}
		})
	}
}
