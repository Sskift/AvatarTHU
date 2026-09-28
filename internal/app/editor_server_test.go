package app

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"testing"
	"time"
)

func waitEditorState(t *testing.T, a *App, want string) M {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		state := readMap(a.data("editor-server.json"))
		if str(state, "state") == want {
			return state
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("editor did not become %s", want)
	return nil
}

func TestEditorURLAndBrowserSessionSurviveRestart(t *testing.T) {
	a, _ := editorFixture(t)
	stop := a.startEditor(a.Ctx)
	t.Cleanup(func() { stop() })
	first := waitEditorState(t, a, "running")
	u, err := url.Parse(str(first, "url"))
	check(err)
	params, err := url.ParseQuery(u.Fragment)
	check(err)
	jar, err := cookiejar.New(nil)
	check(err)
	client := &http.Client{Jar: jar, Timeout: 2 * time.Second}
	req, err := http.NewRequest("POST", "http://"+u.Host+"/session", nil)
	check(err)
	req.Header.Set("X-Editor-Token", params.Get("token"))
	res, err := client.Do(req)
	check(err)
	res.Body.Close()
	if res.StatusCode != 204 {
		t.Fatal("could not establish browser session")
	}
	stop()
	if str(readMap(a.data("editor-server.json")), "url") != str(first, "url") {
		t.Fatal("shutdown discarded the bookmark")
	}
	b := New(context.Background(), a.Root)
	stop = b.startEditor(b.Ctx)
	second := waitEditorState(t, b, "running")
	if str(first, "url") != str(second, "url") {
		t.Fatal("restart changed editor URL")
	}
	res, err = client.Get("http://" + u.Host + "/api/tasks")
	check(err)
	_, _ = io.Copy(io.Discard, res.Body)
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatal("existing browser cookie stopped working after restart", res.StatusCode)
	}
}

func TestEditorMigratesOldURLAndWaitsForOccupiedPort(t *testing.T) {
	a := testApp(t)
	occupied, err := net.Listen("tcp4", "127.0.0.1:0")
	check(err)
	defer occupied.Close()
	raw := "http://" + occupied.Addr().String() + "/#token=" + randomID(32)
	writeJSON(a.data("editor-server.json"), M{"state": "running", "url": raw})
	writeJSON(a.data("schedule.json"), M{"last_sync_at": stamp()})
	ctx, cancel := context.WithCancel(a.Ctx)
	a.Ctx = ctx
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); a.daemon() }()
	state := waitEditorState(t, a, "unavailable")
	if str(state, "url") != raw || str(state, "error") == "" || str(readMap(a.data("daemon-health.json")), "state") != "running" {
		t.Fatal("port conflict changed the URL or stopped the scheduler", state)
	}
	expectError(t, "固定地址", func() { a.openEditor("", true) })
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("port retry blocked daemon shutdown")
	}
	occupied.Close()
	stop := a.startEditor(context.Background())
	defer stop()
	recovered := waitEditorState(t, a, "running")
	if str(recovered, "url") != raw {
		t.Fatal("recovery changed the migrated bookmark")
	}
}
