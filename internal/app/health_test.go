package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The test executable doubles as native CLI fixtures on macOS and Windows.
func TestMain(m *testing.M) {
	if os.Getenv("CODEX_ENGINE_TEST_CLI") == "1" {
		runCodexEngineTestCLI()
		return
	}
	if mode := os.Getenv("AVATARTHU_TEST_EVENT_CLI"); mode != "" {
		if !strings.Contains(strings.Join(os.Args, " "), "--profile example") {
			os.Exit(3)
		}
		if mode == "management" {
			switch os.Args[1] {
			case "auth":
				fmt.Print(`{"appId":"example-app","identities":{"user":{"available":true}}}`)
			case "whoami":
				fmt.Print(`{"profile":"example","onBehalfOf":{"openId":"new-owner"},"available":true}`)
			case "event":
				fmt.Print(`{"ok":true,"data":{"decision":{}}}`)
			default:
				os.Exit(4)
			}
			os.Exit(0)
		}
		if mode == "doc-auth-stderr" || mode == "doc-auth-stdout" {
			fmt.Fprintln(os.Stderr, "lark-cli: user token refresh failed")
			output := os.Stderr
			if mode == "doc-auth-stdout" {
				output = os.Stdout
			}
			fmt.Fprintln(output, `{
  "ok": false,
  "error": {"type":"authentication","message":"need_user_authorization (user: example)"}
}`)
			os.Exit(3)
		}
		if mode == "conflict" {
			fmt.Fprintln(os.Stderr, `{"ok":false,"error":{"message":"another event bus is already connected to this app (1 remote event connection(s) detected via API)"}}`)
			os.Exit(2)
		}
		if mode == "connection" {
			switch strings.Join(os.Args[1:], " ") {
			case "api GET /open-apis/event/v1/connection --as bot --profile example":
				fmt.Print(os.Getenv("AVATARTHU_TEST_CONNECTION_REPLY"))
			case "event stop --json --profile example":
				check(os.WriteFile("idle-bus-stop", []byte("requested"), 0600))
				// A third-party consumer keeps the shared bus alive.
				fmt.Println(`{"results":[{"status":"refused"}]}`)
				os.Exit(2)
			default:
				os.Exit(4)
			}
			os.Exit(0)
		}
		fmt.Fprintln(os.Stderr, "[event] ready event_key=card.action.trigger")
		fmt.Fprintln(os.Stderr, "[source] feishu-websocket: connected")
		io.Copy(io.Discard, os.Stdin)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestLarkUserAuthErrorSurvivesCLIDiagnostics(t *testing.T) {
	a := testApp(t)
	exe, err := os.Executable()
	check(err)
	for _, mode := range []string{"doc-auth-stderr", "doc-auth-stdout"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("AVATARTHU_TEST_EVENT_CLI", mode)
			expectError(t, larkUserLoginRequired, func() {
				a.cliJSON(exe, "docs", "+create", "--profile", "example")
			})
		})
	}
}

func TestLarkLoginPinsProfileAndReadsBareIdentity(t *testing.T) {
	a := testApp(t)
	exe, err := os.Executable()
	check(err)
	t.Setenv("AVATARTHU_TEST_EVENT_CLI", "management")
	a.saveConfig(M{"lark_cli": exe, "lark_user_id": "old-owner", "lark_profile": "old-app", "review_mode": "codex-claude"})
	st := frozen(t, a)
	before := string(jsonBytes(readMap(a.taskPath(str(st, "task_id")))))
	a.loginLark(true, "example")
	cfg := a.config()
	if str(cfg, "lark_profile") != "example" || str(cfg, "lark_user_id") != "new-owner" || str(cfg, "review_mode") != "codex-claude" {
		t.Fatal(cfg)
	}
	if before != string(jsonBytes(readMap(a.taskPath(str(st, "task_id"))))) {
		t.Fatal("login modified frozen homework or card receipts")
	}
}

func TestListenerConflictAndSuccessfulReconnect(t *testing.T) {
	a := testApp(t)
	exe, err := os.Executable()
	check(err)
	a.saveConfig(M{"lark_cli": exe, "lark_enabled": true, "lark_profile": "example"})
	t.Setenv("AVATARTHU_TEST_EVENT_CLI", "conflict")
	err = attempt(func() {
		a.consume(a.Ctx, newLarkConnection(a, a.config()), "card.action.trigger", "actions", func(M) bool { t.Error("unexpected event"); return false })
	})
	if err == nil || !strings.Contains(err.Error(), "another event bus") {
		t.Fatalf("lost child diagnostic: %v", err)
	}
	if a.listenerFailed("actions", err) != 15*time.Minute {
		t.Fatal("conflict must back off")
	}
	first := readMap(a.data("actions-health.json"))
	if str(first, "state") != "failed" || str(first, "category") != "conflict" {
		t.Fatal(first)
	}
	a.reconnectLark()
	if str(readMap(a.data("actions-health.json")), "state") != "failed" {
		t.Fatal("request falsely cleared failure")
	}
	a.listenerFailed("actions", err)
	second := readMap(a.data("actions-health.json"))
	if number(second, "consecutive_failures", 0) != 2 || second["first_failure_at"] != first["first_failure_at"] {
		t.Fatal(second)
	}
	t.Setenv("AVATARTHU_TEST_EVENT_CLI", "ready")
	ctx, cancel := context.WithCancel(a.Ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- attempt(func() {
			a.consume(ctx, newLarkConnection(a, a.config()), "card.action.trigger", "actions", func(M) bool { return false })
		})
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(15 * time.Second):
			t.Error("listener did not stop")
		}
	})
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if h := readMap(a.data("actions-health.json")); str(h, "state") == "ready" {
			if str(h, "reason") != "" || number(h, "consecutive_failures", -1) != 0 {
				t.Fatal("success did not clear failure", h)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("listener never became ready")
}

func TestLarkSocketHealthIsSharedAcrossActiveSubscriptions(t *testing.T) {
	a := testApp(t)
	c := newLarkConnection(a, M{})
	health := func(name string) M { return readMap(a.data(name + "-health.json")) }
	c.observe("actions", "card.action.trigger", 21, "[event] ready event_key=card.action.trigger")
	if str(health("actions"), "state") != "connecting" {
		t.Fatal("local subscription was mistaken for an online callback")
	}
	c.observe("actions", "card.action.trigger", 21, "[source] feishu-websocket: connected")
	c.observe("messages", "im.message.receive_v1", 22, "[event] ready event_key=im.message.receive_v1")
	lastReady := str(health("messages"), "last_ready_at")
	if lastReady == "" || str(health("messages"), "state") != "ready" {
		t.Fatal("late subscriber missed existing connection")
	}
	c.observe("messages", "im.message.receive_v1", 22, "[source] feishu-websocket: disconnected")
	c.observe("actions", "card.action.trigger", 21, "[event] ready event_key=card.action.trigger")
	c.observe("actions", "card.action.trigger", 21, "[source] feishu-websocket: reconnecting (attempt 2)")
	if str(health("actions"), "state") != "reconnecting" || str(health("messages"), "state") != "reconnecting" || str(health("messages"), "last_ready_at") != lastReady {
		t.Fatal("disconnect was lost while consumers stayed alive")
	}
	c.unsubscribe("messages")
	c.observe("actions", "card.action.trigger", 21, "[source] feishu-websocket: connected")
	if str(health("actions"), "state") != "ready" || str(health("messages"), "state") == "ready" {
		t.Fatal("recovery should apply only to subscribed listeners")
	}
}

func TestLarkChecksPlatformBeforeReconnectingIdleBus(t *testing.T) {
	a := testApp(t)
	exe, err := os.Executable()
	check(err)
	c := newLarkConnection(a, M{"lark_cli": exe, "lark_profile": "example"})
	t.Setenv("AVATARTHU_TEST_EVENT_CLI", "connection")
	c.observe("actions", "card.action.trigger", 21, "[event] ready event_key=card.action.trigger")
	now := time.Now()
	t.Setenv("AVATARTHU_TEST_CONNECTION_REPLY", `{"ok":true,"data":{"online_instance_cnt":1}}`)
	if c.needsReconnect(a.Ctx, now) || c.state != "ready" {
		t.Fatal("could not verify an existing connection")
	}
	t.Setenv("AVATARTHU_TEST_CONNECTION_REPLY", `{"ok":true,"data":{}}`)
	if c.needsReconnect(a.Ctx, now.Add(5*time.Minute)) || c.state != "ready" {
		t.Fatal("missing count was treated as an offline connection")
	}
	t.Setenv("AVATARTHU_TEST_CONNECTION_REPLY", `{"ok":true,"data":{"online_instance_cnt":0}}`)
	lost := now.Add(10 * time.Minute)
	if c.needsReconnect(a.Ctx, lost) || c.state != "reconnecting" {
		t.Fatal("give the SDK time to reconnect after the first zero")
	}
	if c.needsReconnect(a.Ctx, lost.Add(15*time.Second)) || !c.needsReconnect(a.Ctx, lost.Add(time.Minute)) {
		t.Fatal("persistent offline connection did not request repair")
	}
	c.stopIdleBus(a.Ctx)
	if !exists(filepath.Join(a.Root, "idle-bus-stop")) {
		t.Fatal("expected an unforced stop for this profile")
	}
	c.observe("actions", "card.action.trigger", 21, "[source] feishu-websocket: disconnected")
	t.Setenv("AVATARTHU_TEST_CONNECTION_REPLY", `{"ok":true,"data":{"online_instance_cnt":1}}`)
	if c.needsReconnect(a.Ctx, lost.Add(2*time.Minute)) || c.state != "reconnecting" {
		t.Fatal("another online instance should not mask a reported disconnect")
	}
	c.unsubscribe("actions")
	if c.needsReconnect(a.Ctx, lost.Add(10*time.Minute)) || c.state != "connecting" {
		t.Fatal("detached listeners should not claim the bus is online or restart it")
	}
}

func TestListenerFailureBackoffAndCategories(t *testing.T) {
	a := testApp(t)
	for i, seconds := range []int{60, 120, 240, 480, 900, 900} {
		if delay := a.listenerFailed("messages", fmt.Errorf("connection timeout")); delay != time.Duration(seconds)*time.Second {
			t.Fatalf("retry %d: %v", i, delay)
		}
	}
	for message, category := range map[string]string{"token expired": "auth", "forbidden scope": "permission", "unknown flag": "version", "未找到 Lark CLI": "missing"} {
		if str(listenerDiagnosis(message), "category") != category {
			t.Fatal(message)
		}
	}
}

func TestDaemonHeartbeatAndStageDeadline(t *testing.T) {
	a := testApp(t)
	now := time.Now()
	a.monitor = &daemonMonitor{app: a, phase: "等待调度", started: now, deadline: now.Add(3 * time.Minute)}
	finish := a.stage("claude writer", 2*time.Hour)
	a.monitor.beat()
	health := readMap(a.data("daemon-health.json"))
	if problem := daemonProblem(health, now); problem != "" {
		t.Fatal(problem)
	}
	// A fresh heartbeat does not conceal an overdue worker stage.
	health["time"] = now.Add(3 * time.Hour).Format(time.RFC3339Nano)
	if problem := daemonProblem(health, now.Add(3*time.Hour)); !strings.Contains(problem, "阶段超时") {
		t.Fatal(problem)
	}
	health["time"] = now.Add(-2 * time.Minute).Format(time.RFC3339Nano)
	if problem := daemonProblem(health, now); !strings.Contains(problem, "心跳") {
		t.Fatal(problem)
	}
	finish()
	health = readMap(a.data("daemon-health.json"))
	if str(health, "phase") != "等待调度" || daemonProblem(health, time.Now()) != "" {
		t.Fatal(health)
	}
}

func TestResendPreservesArtifactsAndRetriesSameMessage(t *testing.T) {
	a := testApp(t)
	a.saveConfig(M{"lark_enabled": true, "lark_profile": "example", "lark_user_id": "owner"})
	st := frozen(t, a)
	merge(st, M{"status": "awaiting", "review_doc": M{"verified": true, "url": "https://example.test/review"}, "card_message_id": "old", "deliveries": M{"card": M{"message_id": "old"}}})
	a.saveTask(st)
	hash, nonce := str(st, "sha256"), str(st, "nonce")
	a.Upload = func(*School, M, string) M { t.Fatal("resend uploaded homework"); return nil }
	a.CallLark = func(args []string) M {
		if strings.Join(args, " ") != "docs +fetch --as user --doc https://example.test/review --scope outline --profile example" {
			t.Fatal(args)
		}
		return M{}
	}
	sends, idem := 0, ""
	a.SendCard = func(card M, key string) M {
		sends++
		if sends == 1 {
			idem = key
			panic(fmt.Errorf("connection timeout"))
		}
		if key != idem {
			t.Fatal("retry could duplicate card")
		}
		if !strings.Contains(string(jsonBytes(card)), "回调尚未就绪") {
			t.Fatal("missing connection warning")
		}
		return M{"message_id": "new", "chat_id": "chat"}
	}
	tid := str(st, "task_id")
	expectError(t, "timeout", func() { a.resendReview(tid) })
	if str(readMap(a.taskPath(tid)), "card_message_id") != "old" {
		t.Fatal("failed send replaced current card")
	}
	a.resendReview(tid)
	saved := readMap(a.taskPath(tid))
	if str(saved, "sha256") != hash || str(saved, "nonce") != nonce || str(saved, "status") != "awaiting" || str(saved, "card_message_id") != "new" || len(objects(saved["card_history"])) != 1 {
		t.Fatal(saved)
	}
	saved["status"] = "submission_unknown"
	a.saveTask(saved)
	expectError(t, "待审阅", func() { a.resendReview(tid) })
}
