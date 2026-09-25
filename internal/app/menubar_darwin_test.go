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
