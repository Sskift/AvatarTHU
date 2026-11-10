package app

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func keepaliveTestSession(value string) M {
	return M{"all_cookies": []M{
		{"name": "JSESSIONID", "value": value, "domain": "learn.tsinghua.edu.cn", "path": "/"},
		{"name": "XSRF-TOKEN", "value": value, "domain": "learn.tsinghua.edu.cn", "path": "/"},
	}}
}

func TestRemoteKeepaliveUpdatesSessionWithoutOverwritingConcurrentLogin(t *testing.T) {
	for _, concurrent := range []bool{false, true} {
		t.Run(fmt.Sprint(concurrent), func(t *testing.T) {
			a := testApp(t)
			writeJSON(a.session(), keepaliveTestSession("original"))
			original, _ := os.ReadFile(a.session())
			want := keepaliveTestSession("remote")
			if concurrent {
				want = keepaliveTestSession("new-login")
				writeJSON(a.session(), want)
			}
			a.applyRemoteKeepalive(original, M{"status": M{"state": "valid", "checked_at": stamp()}, "session": keepaliveTestSession("remote")})
			if !reflect.DeepEqual(readMap(a.session()), parseMap(jsonBytes(want))) {
				t.Fatal("remote response lost the expected session")
			}
		})
	}
}

func TestRemoteKeepaliveRejectsStaleOrInvalidSessionAndPreservesLocalBytes(t *testing.T) {
	for _, result := range []M{
		{"status": M{"state": "valid", "checked_at": time.Now().Add(-time.Hour).Format(time.RFC3339)}, "session": keepaliveTestSession("old")},
		{"status": M{"state": "valid", "checked_at": stamp()}, "session": M{}},
		{"status": M{"state": "other"}},
	} {
		a := testApp(t)
		writeJSON(a.session(), keepaliveTestSession("local"))
		original, _ := os.ReadFile(a.session())
		if err := attempt(func() { a.applyRemoteKeepalive(original, result) }); err == nil {
			t.Fatal("accepted invalid remote response")
		}
		after, _ := os.ReadFile(a.session())
		if !bytes.Equal(original, after) {
			t.Fatal("invalid remote response changed session bytes")
		}
	}
}

func TestRemoteExpiredResponseDoesNotReplaceLocalSession(t *testing.T) {
	a := testApp(t)
	writeJSON(a.session(), keepaliveTestSession("local"))
	original, _ := os.ReadFile(a.session())
	a.applyRemoteKeepalive(original, M{"status": M{"state": "expired"}, "session": keepaliveTestSession("anonymous")})
	after, _ := os.ReadFile(a.session())
	if !bytes.Equal(original, after) {
		t.Fatal("expired response replaced local session")
	}
}

func TestRemoteLoginAcknowledgementOnlyClearsItsOwnPendingRequest(t *testing.T) {
	for _, currentID := range []string{"sent", "new-login"} {
		a := testApp(t)
		writeJSON(a.session(), keepaliveTestSession("local"))
		original, _ := os.ReadFile(a.session())
		writeJSON(a.remoteLoginPendingPath(), M{"id": currentID})
		a.receiveRemoteKeepalive(original, "sent", M{"accepted": true, "status": M{"state": "valid", "checked_at": stamp()}, "session": keepaliveTestSession("remote")})
		if exists(a.remoteLoginPendingPath()) != (currentID == "new-login") {
			t.Fatal("acknowledgement lost a newer pending login or failed to clear its own")
		}
	}
	a := testApp(t)
	writeJSON(a.session(), keepaliveTestSession("new-login"))
	original, _ := os.ReadFile(a.session())
	writeJSON(a.remoteLoginPendingPath(), M{"id": "pending"})
	expectError(t, "未接受", func() {
		a.receiveRemoteKeepalive(original, "pending", M{"accepted": false, "status": M{"state": "valid", "checked_at": stamp()}, "session": keepaliveTestSession("old-remote")})
	})
	after, _ := os.ReadFile(a.session())
	if !bytes.Equal(original, after) || !exists(a.remoteLoginPendingPath()) {
		t.Fatal("failed remote import lost the new local login or pending request")
	}
}

func TestKeepaliveExchangePrefersLiveRemoteOverStaleClient(t *testing.T) {
	a := testApp(t)
	writeJSON(a.session(), keepaliveTestSession("live-remote"))
	writeJSON(filepath.Join(a.Root, "keepalive-status.json"), M{"state": "valid", "checked_at": stamp()})
	result := a.exchangeKeepalive(M{"session": keepaliveTestSession("stale-client")})
	if boolean(result, "accepted") || !reflect.DeepEqual(obj(result, "session"), readMap(a.session())) {
		t.Fatal("stale client displaced the live remote session")
	}
}

func TestKeepaliveSessionValidationFailurePreservesRemoteState(t *testing.T) {
	a := testApp(t)
	writeJSON(a.session(), keepaliveTestSession("original"))
	original, _ := os.ReadFile(a.session())
	expectError(t, "rejected", func() {
		a.acceptKeepaliveSession(keepaliveTestSession("invalid"), func(M) M { panic(sessionExpired{"rejected"}) })
	})
	after, _ := os.ReadFile(a.session())
	if !bytes.Equal(original, after) || exists(filepath.Join(a.Root, "keepalive-status.json")) {
		t.Fatal("failed authentication changed the remote session or status")
	}
	a.acceptKeepaliveSession(keepaliveTestSession("valid"), func(m M) M { return m })
	if str(readMap(filepath.Join(a.Root, "keepalive-status.json")), "state") != "valid" {
		t.Fatal("validated session was not saved")
	}
}

func TestKeepaliveImportPreservesLoginCompletedDuringValidation(t *testing.T) {
	a := testApp(t)
	writeJSON(a.session(), keepaliveTestSession("original"))
	expectError(t, "已更新", func() {
		a.acceptKeepaliveSession(keepaliveTestSession("candidate"), func(m M) M {
			writeJSON(a.session(), keepaliveTestSession("new-login"))
			return m
		})
	})
	if !reflect.DeepEqual(readMap(a.session()), parseMap(jsonBytes(keepaliveTestSession("new-login")))) {
		t.Fatal("session import overwrote a newer login")
	}
}

func TestRemoteKeepaliveArgumentsRejectShellAndOptionInjection(t *testing.T) {
	remoteKeepaliveArgs(M{"host": "home", "binary": "C:/Users/skift/.avatarthu/bin/avatarthu.exe"})
	for _, config := range []M{
		{"host": "-oProxyCommand=bad", "binary": "/bin/avatarthu"},
		{"host": "home;bad", "binary": "/bin/avatarthu"},
		{"host": "home", "binary": "/bin/avatar;bad"},
		{"host": "home", "binary": "$(bad)"},
		{"host": "home", "binary": "/path with spaces/avatar"},
	} {
		if err := attempt(func() { remoteKeepaliveArgs(config) }); err == nil {
			t.Fatal("accepted shell syntax or SSH option as configuration")
		}
	}
}

func TestKeepaliveOnlyDaemonDoesNotStartEditorOrCourseWork(t *testing.T) {
	a := testApp(t)
	ctx, cancel := context.WithCancel(a.Ctx)
	a.Ctx = ctx
	defer cancel()
	writeJSON(filepath.Join(a.Root, "keepalive-status.json"), M{"state": "valid", "checked_at": stamp()})
	done := make(chan struct{})
	go func() { defer close(done); a.daemonMode(true) }()
	deadline := time.Now().Add(5 * time.Second)
	for str(readMap(a.data("daemon-health.json")), "state") != "running" {
		if time.Now().After(deadline) {
			t.Fatal("keepalive daemon did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("keepalive daemon did not stop")
	}
	for _, file := range []string{"editor-server.json", "schedule.json", "sync-result.json", "run-result.json", "actions-health.json", "messages-health.json"} {
		if exists(a.data(file)) {
			t.Fatalf("keepalive-only daemon started unrelated work: %s", file)
		}
	}
}
