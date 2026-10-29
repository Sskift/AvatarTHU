package app

import (
	"path/filepath"
	"strings"
)

func (a *App) ignoreTask(tid, reason string) {
	path := a.taskPath(tid)
	defer a.lock(tid, false)()
	defer a.lock("editor-"+tid, false)()
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
	requests, err := filepath.Glob(filepath.Join(a.editorDir(st), "requests", "*", "request.json"))
	check(err)
	for _, path := range requests {
		ensure(str(readMap(path), "state") != "running", "报告局部修改正在处理，请等处理结束后再忽略")
	}
	merge(st, M{"ignored_from_status": st["status"], "ignored_reason": reason, "ignored_at": stamp(), "status": "ignored"})
	a.saveTask(st)
}

func (a *App) restoreTask(tid string) {
	path := a.taskPath(tid)
	defer a.lock(tid, false)()
	st := readMap(path)
	ensure(len(st) > 0, "找不到这份作业")
	if str(st, "status") != "ignored" {
		return
	}
	status := str(st, "ignored_from_status")
	switch status {
	case "queued", "revision_ready", "working", "rewriting", "reviewing", "delivery_pending", "failed", "awaiting", "needs_student", "approval_invalid", "closed_remote":
	default:
		panic("忽略前的状态无效，无法恢复作业")
	}
	// Sync preserves ignored tasks, while the downloaded materials may change.
	// Recheck the latest cached assignment before reactivating an old version.
	meta := readMap(assignmentPath(str(st, "assignment_dir"), "assignment.json"))
	if len(meta) == 0 {
		meta = st
	}
	hash := materialsHash(meta)
	if status != "closed_remote" && (str(st, "source_hash") != hash || boolean(st, "source_cached")) {
		status = "revision_ready"
		st["feedback"] = "忽略期间课程材料或输入资料有更新，请重新核对"
	}
	history := append(objects(st["ignore_history"]), M{"ignored_at": st["ignored_at"], "reason": st["ignored_reason"], "from_status": st["ignored_from_status"], "restored_at": stamp(), "restored_status": status})
	merge(st, meta)
	merge(st, M{"status": status, "source_hash": hash, "source_cached": false, "ignore_history": history})
	for _, key := range []string{"ignored_at", "ignored_reason", "ignored_from_status"} {
		delete(st, key)
	}
	a.saveTask(st)
	a.wakeEditor()
}
