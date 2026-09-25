package app

import (
	"errors"
	"fmt"
	"path/filepath"
	"time"
)

type toolFailure struct {
	Tool, Category, Reason, Action, Log string
	Retryable                           bool
}

func (e *toolFailure) Error() string {
	message := e.Tool + "：" + e.Reason + "。" + e.Action
	if e.Log != "" {
		message += "；日志：" + e.Log
	}
	return message
}

func failureFields(err error) M {
	var failure *toolFailure
	if !errors.As(err, &failure) {
		return M{}
	}
	return M{"tool": failure.Tool, "category": failure.Category, "reason": failure.Reason,
		"action": failure.Action, "log": failure.Log, "retryable": failure.Retryable}
}

func (a *App) recordToolFailure(err error, role, job string) {
	value := failureFields(err)
	if len(value) == 0 {
		return
	}
	merge(value, M{"state": "failed", "phase": role, "job": job, "time": stamp()})
	writeJSON(a.data(str(value, "tool")+"-execution.json"), value)
}

func (a *App) requireUnblockedTools() {
	for _, tool := range []string{"claude", "codex"} {
		value := readMap(a.data(tool + "-execution.json"))
		if str(value, "state") == "failed" && !boolean(value, "retryable") && str(value, "retry_requested_at") == "" {
			panic(&toolFailure{Tool: tool, Category: str(value, "category"), Reason: str(value, "reason"),
				Action: str(value, "action") + "；处理后从菜单栏恢复，或运行 avatarthu retry --tool " + tool, Log: str(value, "log")})
		}
	}
}

func (a *App) saveTaskFailure(st M, err error) {
	st["resume_status"] = st["status"]
	failure := failureFields(err)
	merge(st, M{"status": "failed", "error": safeError(err), "failure": failure,
		"retry_blocked": len(failure) > 0 && !boolean(failure, "retryable"),
		"retry_at":      time.Now().Add(15 * time.Minute).Format(time.RFC3339)})
	a.saveTask(st)
}

func taskRetryDue(st M, tick bool) bool {
	if str(st, "status") != "failed" {
		return true
	}
	if boolean(st, "retry_blocked") {
		return false
	}
	return !tick || !parseTime(str(st, "retry_at")).After(time.Now())
}

// Queue only failed model work. This command never executes upload actions.
func (a *App) retryTool(tool string) int {
	ensure(tool == "claude" || tool == "codex", "用法：avatarthu retry --tool claude|codex")
	if a.RunModel == nil {
		for _, result := range a.inspectPair(a.pairing()) {
			ensure(boolean(result, "usable"), str(result, "tool")+"："+str(result, "reason"))
		}
	}
	defer a.lock("model-worker", false)()
	health := readMap(a.data(tool + "-execution.json"))
	if str(health, "state") == "failed" {
		health["retry_requested_at"] = stamp()
		writeJSON(a.data(tool+"-execution.json"), health)
	}
	count := 0
	for _, task := range a.tasks() {
		if str(task, "status") != "failed" || str(obj(task, "failure"), "tool") != tool {
			continue
		}
		func() {
			defer a.lock(str(task, "task_id"), false)()
			st := readMap(a.taskPath(str(task, "task_id")))
			if str(st, "status") != "failed" || str(obj(st, "failure"), "tool") != tool {
				return
			}
			merge(st, M{"retry_blocked": false, "retry_at": stamp(), "retry_requested_at": stamp()})
			a.saveTask(st)
			count++
		}()
	}
	fmt.Printf("已恢复 %s 的 %d 项失败作业；后台下一分钟从已完成阶段继续。尚未发出模型请求，实际成功后才清除执行异常。\n", tool, count)
	if !a.serviceLoaded() {
		fmt.Println("后台未启动，请运行 avatarthu service start。")
	}
	return count
}

func executionLog(job, tool string) string { return filepath.Join(job, tool+".stderr.log") }
