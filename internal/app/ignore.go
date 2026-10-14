package app

import "strings"

func (a *App) ignoreTask(tid, reason string) {
	path := a.taskPath(tid)
	defer a.lock(tid, false)()
	st := readMap(path)
	ensure(len(st) > 0, "找不到这份作业")
	reason = strings.TrimSpace(reason)
	ensure(reason != "", "忽略原因不能为空")
	switch str(st, "status") {
	case "submitted", "submitting", "submission_unknown":
		panic("已有提交记录或提交结果待确认，不能改为忽略")
	case "ignored":
		return
	}
	merge(st, M{"ignored_from_status": st["status"], "ignored_reason": reason, "ignored_at": stamp(), "status": "ignored"})
	a.saveTask(st)
}
