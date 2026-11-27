package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// Both CLI subscriptions share one Feishu WebSocket. The CLI's ready marker
// only acknowledges a subscription to its local event bus.
type larkConnection struct {
	app          *App
	cli          string
	profile      string
	mu           sync.Mutex
	pids         map[string]int
	state        string
	offlineSince time.Time
	// A source transition received during a remote check supersedes its result.
	sequence   uint64
	sourceDown bool
	lastCheck  time.Time
}

func newLarkConnection(a *App, cfg M) *larkConnection {
	return &larkConnection{app: a, cli: str(cfg, "lark_cli"), profile: str(cfg, "lark_profile"),
		pids: make(map[string]int), state: "connecting"}
}

func (c *larkConnection) record(name string) {
	file := c.app.data(name + "-health.json")
	h := readMap(file)
	now := stamp()
	if c.state == "ready" {
		h = M{"last_ready_at": now, "consecutive_failures": 0}
	}
	merge(h, M{"state": c.state, "pid": c.pids[name], "time": now})
	writeJSON(file, h)
}

func (c *larkConnection) setState(state string, now time.Time) {
	if c.state == state {
		return
	}
	c.state = state
	c.offlineSince = time.Time{}
	if state == "reconnecting" {
		c.offlineSince = now
	}
	for name := range c.pids {
		c.record(name)
	}
}

func (c *larkConnection) observe(name, key string, pid int, line string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if line == "[event] ready event_key="+key {
		c.pids[name] = pid
		c.record(name)
		return
	}
	source, ok := strings.CutPrefix(line, "[source] feishu-websocket: ")
	if !ok {
		return
	}
	status, _, _ := strings.Cut(source, " ")
	switch status {
	case "connected":
		c.sourceDown = false
		c.setState("ready", time.Now())
	case "connecting":
		c.sourceDown = true
		c.setState("connecting", time.Now())
	case "disconnected", "reconnecting":
		c.sourceDown = true
		c.setState("reconnecting", time.Now())
	default:
		return
	}
	c.sequence++
}

func (c *larkConnection) unsubscribe(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.pids, name)
	if len(c.pids) == 0 {
		c.setState("connecting", time.Now())
		c.sourceDown = false
		c.lastCheck = time.Time{}
		c.sequence++
	}
}

// Late subscribers don't receive past connection transitions. Check Feishu
// itself on startup and periodically to catch half-open connections after sleep.
func (c *larkConnection) needsReconnect(ctx context.Context, now time.Time) bool {
	c.mu.Lock()
	interval := time.Minute
	if c.state == "ready" {
		interval = 5 * time.Minute
	}
	if len(c.pids) == 0 || now.Sub(c.lastCheck) < interval {
		c.mu.Unlock()
		return false
	}
	c.lastCheck = now
	sequence := c.sequence
	c.mu.Unlock()

	out, err := c.command(ctx, "api", "GET", "/open-apis/event/v1/connection", "--as", "bot")
	var reply struct {
		OK   bool
		Data struct {
			Count *int `json:"online_instance_cnt"`
		}
	}
	if err != nil || json.Unmarshal(out, &reply) != nil || !reply.OK || reply.Data.Count == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sequence != sequence || len(c.pids) == 0 {
		return false
	}
	switch *reply.Data.Count {
	case 0:
		c.setState("reconnecting", now)
		return now.Sub(c.offlineSince) >= time.Minute
	case 1:
		if !c.sourceDown {
			c.setState("ready", now)
		}
	}
	return false
}

func (c *larkConnection) argv(args ...string) []string {
	exe := findExecutable("lark-cli", c.cli)
	ensure(exe != "", "未找到 Lark CLI")
	return append([]string{exe}, larkProfileArgs(c.profile, args...)...)
}

func (c *larkConnection) command(ctx context.Context, args ...string) ([]byte, error) {
	var out []byte
	err := attempt(func() {
		var err error
		out, _, err = capture(ctx, c.app.Root, 8*time.Second, c.argv(args...)...)
		check(err)
	})
	return out, err
}

func (c *larkConnection) stopIdleBus(ctx context.Context) {
	// The CLI refuses this request when other consumers still use the bus.
	// Without it an immediate restart can reattach to the same broken socket.
	if _, err := c.command(ctx, "event", "stop", "--json"); err != nil && ctx.Err() == nil {
		fmt.Fprintln(os.Stderr, "未关闭飞书事件服务，将重新订阅当前应用："+safeError(err))
	}
}
