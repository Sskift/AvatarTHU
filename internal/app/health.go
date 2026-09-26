package app

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// Heartbeats continue while a model is working. Stage deadlines separately
// expose a stuck scheduler without treating a long, bounded model run as dead.
type daemonMonitor struct {
	mu                sync.Mutex
	app               *App
	phase             string
	started, deadline time.Time
}

func (m *daemonMonitor) writeLocked() {
	writeJSON(m.app.data("daemon-health.json"), M{"state": "running", "pid": os.Getpid(), "time": stamp(), "version": Version,
		"heartbeat_interval_seconds": 15, "phase": m.phase, "phase_started_at": m.started.Format(time.RFC3339Nano), "deadline_at": m.deadline.Format(time.RFC3339Nano)})
}
func (m *daemonMonitor) beat() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.writeLocked()
}
func (m *daemonMonitor) loop(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.beat()
		}
	}
}
func (a *App) stage(phase string, limit time.Duration) func() {
	m := a.monitor
	if m == nil {
		return func() {}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	previous, previousLimit := m.phase, m.deadline.Sub(m.started)
	m.phase, m.started, m.deadline = phase, time.Now(), time.Now().Add(limit)
	m.writeLocked()
	return func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		m.phase, m.started, m.deadline = previous, time.Now(), time.Now().Add(previousLimit)
		m.writeLocked()
	}
}
func daemonProblem(h M, now time.Time) string {
	if str(h, "state") != "running" {
		return ""
	}
	if number(h, "heartbeat_interval_seconds", 0) > 0 {
		checked := parseTime(str(h, "time"))
		if checked.IsZero() || now.Sub(checked) > 90*time.Second || checked.Sub(now) > time.Minute {
			return "后台心跳未更新，请查看日志和进程状态"
		}
		deadline := parseTime(str(h, "deadline_at"))
		if !deadline.IsZero() && now.After(deadline) {
			return "调度阶段超时（" + str(h, "phase") + "），请查看日志；不会自动重启或重新提交作业"
		}
	}
	return ""
}

func listenerDiagnosis(message string) M {
	low := strings.ToLower(message)
	category, reason, action := "connection", "飞书连接中断", "检查网络、代理和 Lark CLI 状态；可点击重连飞书"
	delay := 60
	switch {
	case strings.Contains(low, "another event bus") || strings.Contains(low, "remote event connection detected"):
		category, reason, action, delay = "conflict", "飞书应用连接冲突", "同一飞书应用已被其他监听占用。停止原有监听，或为 AvatarTHU 选择另一个应用的 profile，再重连飞书", 900
	case strings.Contains(low, "未找到") || strings.Contains(low, "no such file") || strings.Contains(low, "executable file not found"):
		category, reason, action, delay = "missing", "Lark CLI 路径无效", "安装 Lark CLI 后运行 avatarthu login lark，再重连飞书", 900
	case strings.Contains(low, "unknown command") || strings.Contains(low, "unknown flag") || strings.Contains(low, "unknown option"):
		category, reason, action, delay = "version", "Lark CLI 版本不兼容", "更新 Lark CLI 后重连飞书", 900
	case strings.Contains(low, "unauthorized") || strings.Contains(low, "token expired") || strings.Contains(low, "not logged") || strings.Contains(low, "invalid app") || strings.Contains(low, "authentication"):
		category, reason, action, delay = "auth", "飞书登录或应用授权失效", "运行 avatarthu login lark 完成当前 profile 的授权，再重连飞书", 900
	case strings.Contains(low, "permission") || strings.Contains(low, "forbidden") || strings.Contains(low, "scope") || strings.Contains(low, "subscription"):
		category, reason, action, delay = "permission", "飞书权限或事件订阅未就绪", "检查当前应用的卡片回调、消息事件和权限设置，再重连飞书", 900
	}
	return M{"category": category, "reason": reason, "action": action, "retry_seconds": delay}
}
func (a *App) listenerFailed(name string, err error) time.Duration {
	p := a.data(name + "-health.json")
	previous := readMap(p)
	next := listenerDiagnosis(err.Error())
	count := number(previous, "consecutive_failures", 0) + 1
	seconds := number(next, "retry_seconds", 60)
	if seconds == 60 {
		for i := 1; i < count && seconds < 900; i++ {
			seconds *= 2
		}
		if seconds > 900 {
			seconds = 900
		}
	}
	first := strDefault(previous, "first_failure_at", stamp())
	merge(next, M{"state": "failed", "time": stamp(), "first_failure_at": first, "consecutive_failures": count,
		"last_ready_at": previous["last_ready_at"], "retry_at": time.Now().Add(time.Duration(seconds) * time.Second).Format(time.RFC3339),
		"retry_seconds": seconds})
	writeJSON(p, next)
	fmt.Fprintln(os.Stderr, name+"："+str(next, "reason")+"。"+str(next, "action"))
	return time.Duration(seconds) * time.Second
}
func (a *App) reconnectLark() {
	ensure(a.enabled(), "飞书未启用，请先运行 avatarthu login lark")
	writeJSON(a.data("lark-reconnect.json"), M{"request_id": randomID(12), "requested_at": stamp()})
	fmt.Println("已请求重连飞书；后台将在 15 秒内重启 AvatarTHU 自己的两个监听。连接成功前保留异常提示，不会停止其他程序或其他设备的连接。")
}
