package app

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"
)

func (a *App) nextScan(schedule M) time.Time {
	if s := str(schedule, "next_sync_retry_at"); s != "" {
		return parseTime(s)
	}
	last := parseTime(str(schedule, "last_sync_at"))
	if last.IsZero() {
		return time.Now()
	}
	return last.Add(time.Duration(number(a.config(), "poll_interval_seconds", 43200)) * time.Second)
}
func (a *App) syncCourses() {
	s := a.school(true)
	notices := a.sendNotices(s.announcements(), s.markRead)
	pending := s.assignments()
	active := map[string]bool{}
	oldTasks := a.tasks()
	for _, meta := range pending {
		tid := fingerprint([]any{meta["semester"], meta["course_id"], meta["xszyid"]})[:16]
		for _, old := range oldTasks {
			if boolean(old, "source_cached") && old["xszyid"] == meta["xszyid"] {
				tid = str(old, "task_id")
				break
			}
		}
		active[tid] = true
		func() {
			defer a.lock(tid, true)()
			st := readMap(a.taskPath(tid))
			hash := materialsHash(meta)
			if len(st) == 0 {
				merge(st, meta)
				merge(st, M{"task_id": tid, "status": "queued", "revision": 1, "source_hash": hash})
			} else {
				switch str(st, "status") {
				case "submitted", "submitting", "submission_unknown":
				default:
					if str(st, "source_hash") != hash || boolean(st, "source_cached") {
						merge(st, M{"status": "revision_ready", "feedback": "课程材料或输入资料有更新，请重新核对"})
					}
					merge(st, meta)
					merge(st, M{"source_hash": hash, "source_cached": false})
				}
			}
			a.saveTask(st)
		}()
	}
	for _, old := range a.tasks() {
		tid := str(old, "task_id")
		if active[tid] {
			continue
		}
		func() {
			defer a.lock(tid, true)()
			st := readMap(a.taskPath(tid))
			switch str(st, "status") {
			case "queued", "awaiting", "needs_student", "revision_ready", "working", "reviewing", "rewriting", "failed", "approval_invalid", "delivery_pending":
				st["status"] = "closed_remote"
				a.saveTask(st)
			}
		}()
	}
	writeJSON(a.data("sync-result.json"), M{"time": stamp(), "pending": len(pending), "notices_sent": notices})
	fmt.Printf("同步完成：%d 项待交作业，%d 条新公告\n", len(pending), notices)
}
func (a *App) run(tick, syncOnly bool, selected string) bool {
	defer a.lock("daily", false)()
	ctx, cancel := context.WithCancel(a.Ctx)
	defer cancel()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); a.maintenanceLoop(ctx) }()
	defer func() { cancel(); wg.Wait() }()
	success := true
	schedule := readMap(a.data("schedule.json"))
	if !tick || !time.Now().Before(a.nextScan(schedule)) {
		e := attempt(a.syncCourses)
		if e == nil {
			writeJSON(a.data("schedule.json"), M{"last_sync_at": stamp()})
		} else {
			success = false
			schedule["next_sync_retry_at"] = time.Now().Add(15 * time.Minute).Format(time.RFC3339)
			writeJSON(a.data("schedule.json"), schedule)
			fmt.Fprintln(os.Stderr, "网络学堂同步失败："+safeError(e))
			_ = attempt(func() {
				a.notifyOnce("sync-error:"+time.Now().In(beijing).Format("2006-01-02")+":"+fingerprint(e.Error()), "网络学堂同步失败："+cut(safeError(e), 1200)+"\n登录过期时运行 avatarthu login thu；已缓存的作业继续处理。")
			})
		}
	}
	if syncOnly {
		return success
	}
	for _, old := range a.tasks() {
		if a.Ctx.Err() != nil {
			return false
		}
		tid := str(old, "task_id")
		if selected != "" && selected != tid {
			continue
		}
		func() {
			defer a.lock(tid, true)()
			st := readMap(a.taskPath(tid))
			status := str(st, "status")
			if status == "awaiting" || status == "needs_student" {
				if e := attempt(func() { a.refreshCard(st) }); e != nil {
					fmt.Fprintln(os.Stderr, "审阅文档稍后重试："+safeError(e))
				}
				return
			}
			if status == "submitted" && a.enabled() && st["receipt_message"] == nil {
				_ = attempt(func() { a.submissionReceipt(st) })
				return
			}
			switch status {
			case "queued", "revision_ready", "working", "reviewing", "rewriting", "delivery_pending", "failed":
			default:
				return
			}
			if status == "failed" && tick && parseTime(str(st, "retry_at")).After(time.Now()) {
				return
			}
			if status == "failed" {
				st["status"] = strDefault(st, "resume_status", "queued")
			}
			e := attempt(func() { a.process(st) })
			if e != nil {
				st["resume_status"] = st["status"]
				merge(st, M{"status": "failed", "error": safeError(e), "retry_at": time.Now().Add(15 * time.Minute).Format(time.RFC3339)})
				a.saveTask(st)
				fmt.Fprintln(os.Stderr, safeError(e))
				if a.Ctx.Err() == nil {
					_ = attempt(func() {
						a.notifyOnce(fmt.Sprintf("task-error:%s:%d:%s", tid, number(st, "revision", 1), fingerprint(e.Error())), "作业处理需要检查："+str(st, "title")+"\n"+cut(safeError(e), 1000)+"\n修复后运行 avatarthu run 从成功阶段继续。")
					})
				}
			}
		}()
	}
	writeJSON(a.data("run-result.json"), M{"completed_at": stamp(), "tasks": len(a.tasks())})
	return success
}
func (a *App) maintenanceLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		if e := attempt(func() { a.keepalive(false, true) }); e != nil {
			fmt.Fprintln(os.Stderr, "保活稍后重试："+safeError(e))
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (a *App) startListeners(parent context.Context) func() {
	ctx, cancel := context.WithCancel(parent)
	var workers sync.WaitGroup
	for _, name := range []string{"actions", "messages"} {
		name := name
		workers.Add(1)
		go func() {
			defer workers.Done()
			handler, key := a.act, "card.action.trigger"
			if name == "messages" {
				handler, key = a.message, "im.message.receive_v1"
			}
			for ctx.Err() == nil {
				e := attempt(func() { a.consume(ctx, key, name, handler) })
				if e != nil && ctx.Err() == nil {
					fmt.Fprintln(os.Stderr, safeError(e))
				}
				select {
				case <-ctx.Done():
					return
				case <-time.After(time.Minute):
				}
			}
		}()
	}
	return func() { cancel(); workers.Wait() }
}
func (a *App) eventManager(ctx context.Context) {
	stop := func() {}
	defer func() { stop() }()
	activeKey := ""
	timer := time.NewTicker(15 * time.Second)
	defer timer.Stop()
	for {
		key := ""
		if a.enabled() {
			cfg := a.config()
			key = str(cfg, "lark_cli") + ":" + str(cfg, "lark_user_id")
		}
		if key != activeKey {
			stop()
			stop = func() {}
			activeKey = key
			if key != "" {
				stop = a.startListeners(ctx)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
	}
}
func (a *App) daemon() {
	defer a.lock("daemon", false)()
	ctx, cancel := context.WithCancel(a.Ctx)
	defer cancel()
	var workers sync.WaitGroup
	workers.Add(2)
	go func() { defer workers.Done(); a.eventManager(ctx) }()
	go func() { defer workers.Done(); a.maintenanceLoop(ctx) }()
	defer func() {
		cancel()
		workers.Wait()
		writeJSON(a.data("daemon-health.json"), M{"state": "stopped", "pid": os.Getpid(), "time": stamp()})
	}()
	for {
		writeJSON(a.data("daemon-health.json"), M{"state": "running", "pid": os.Getpid(), "time": stamp(), "version": Version})
		if e := attempt(func() { a.runScheduled() }); e != nil {
			fmt.Fprintln(os.Stderr, safeError(e))
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Minute):
		}
	}
}

// run already keeps maintenance alive while a manual run is active. In a daemon,
// the same process also has a persistent maintenance goroutine; the lock and due
// timestamp ensure only one request per interval, including during long model jobs.
func (a *App) runScheduled() { a.run(true, false, "") }
