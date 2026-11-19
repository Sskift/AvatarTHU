package app

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func savedActivity(job string) []activityEvent {
	if job == "" {
		return nil
	}
	path := job + "-activity.json"
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var value activityLog
	if json.NewDecoder(io.LimitReader(f, 32<<10)).Decode(&value) != nil {
		return nil
	}
	return value.Events
}

func timelineRow(revision, round int, role, harness, state, title, at string) M {
	return M{"revision": revision, "round": round, "role": role, "harness": harness, "state": state, "title": title, "at": at}
}

func timelineFiles(paths []string) []string {
	names := []string{}
	for _, p := range paths {
		if p != "" {
			names = append(names, filepath.Base(filepath.FromSlash(p)))
		}
	}
	sort.Strings(names)
	if len(names) > 8 {
		names = names[:8]
	}
	return names
}

func optionalRunRecord(job, name string) M {
	var record M
	// Older jobs can have partial receipts. Keep the workspace and owner
	// controls usable even when these optional display details are missing.
	_ = attempt(func() { record = readMap(filepath.Join(job, name)) })
	return record
}

func runTimeline(job string, revision, round int, role, harness string, active bool) []M {
	if job == "" {
		return nil
	}
	name := "主写"
	if role == "reviewer" {
		name = "独立复审"
	}
	process := optionalRunRecord(job, "process.json")
	at := str(process, "started_at")
	if tool := str(process, "executor"); validHarness(tool) {
		harness = tool
	}
	state, title := "done", name+"会话已启动"
	if active {
		state = "active"
		if at == "" {
			title = "准备" + name + "会话"
		}
	}
	events := savedActivity(job)
	if at == "" && len(events) == 0 && !active {
		return nil
	}
	rows := []M{timelineRow(revision, round, role, harness, state, title, at)}
	for _, ev := range events {
		label := activityLabels[ev.Action]
		if label == "" {
			continue
		}
		row := timelineRow(revision, round, role, harness, "done", label, ev.At)
		if ev.File != "" && filepath.Base(ev.File) == ev.File {
			row["files"] = []string{ev.File}
		}
		if ev.Count > 1 {
			row["detail"] = fmt.Sprintf("连续 %d 次", ev.Count)
		}
		rows = append(rows, row)
	}
	return rows
}

// Insert receipts among recorded work in their target version. Some legacy
// stages have no timestamp. A change bound to a writer round still precedes
// that round's candidate even if its finish time was never recorded.
func insertOwnerChanges(rows, changes []M) []M {
	if len(changes) == 0 {
		return rows
	}
	if len(changes) > 24 {
		changes = changes[len(changes)-24:]
	}
	before := make([][]M, len(rows)+1)
	for _, m := range changes {
		base := number(m, "revision", 1)
		version, round := number(m, "target_revision", base), number(m, "round", 0)
		at := str(m, "created_at")
		row := timelineRow(version, round, "system", "", "done", "已收到修改要求", at)
		if files := timelineFiles(texts(m["files"])); len(files) > 0 {
			row["files"] = files
			row["detail"] = fmt.Sprintf("关联第 %d 版文件，目标为第 %d 版", number(m, "file_revision", base), version)
		} else {
			row["detail"] = fmt.Sprintf("目标为第 %d 版", version)
		}
		slot := len(rows)
		received := parseTime(at)
		for i, work := range rows {
			rv, rn := number(work, "revision", 1), number(work, "round", 0)
			if rv < version {
				continue
			}
			workAt := parseTime(str(work, "at"))
			after := !received.IsZero() && !workAt.IsZero() && !workAt.Before(received)
			if rv > version || after || (round > 0 && rn >= round && workAt.IsZero()) {
				slot = i
				break
			}
		}
		before[slot] = append(before[slot], row)
	}
	merged := make([]M, 0, len(rows)+len(changes))
	for i, work := range rows {
		merged = append(merged, before[i]...)
		merged = append(merged, work)
	}
	return append(merged, before[len(rows)]...)
}

func (a *App) workspaceTimeline(st, conversation M) []M {
	rev, round := number(st, "revision", 1), number(st, "review_round", 1)
	rows := []M{}
	status := str(st, "status")
	plan := obj(st, "execution_plan")
	writer, reviewer := str(plan, "writer"), str(plan, "reviewer")
	var changes []M
	changeVersions := map[string]int{}
	for _, m := range objects(conversation["messages"]) {
		id := str(m, "id")
		if str(m, "role") == "user" && str(m, "kind") == "change" && id != "" {
			changeVersions[id] = number(m, "target_revision", number(m, "revision", 1))
			changes = append(changes, m)
		}
	}
	history := objects(st["review_history"])
	// The full review history remains available under its own revision. Bound
	// work in the live poll independently of the assignment's age.
	if len(history) > 24 {
		history = history[len(history)-24:]
	}
	lastReviewed := ""
	for i, receipt := range history {
		rv, rn := number(receipt, "revision", 1), number(receipt, "round", 1)
		lastReviewed = str(receipt, "writer_job")
		recent := i == len(history)-1 && rv == rev
		if recent {
			rows = append(rows, runTimeline(lastReviewed, rv, rn, "writer", str(receipt, "writer"), false)...)
		}
		w := timelineRow(rv, rn, "writer", str(receipt, "writer"), "done", "主写已提供候选产物", "")
		if recent && lastReviewed != "" {
			w["at"] = str(optionalRunRecord(lastReviewed, "complete.json"), "finished_at")
		}
		paths := []string{}
		for p := range obj(receipt, "artifact_hashes") {
			paths = append(paths, p)
		}
		w["files"] = timelineFiles(paths)
		rows = append(rows, w)
		if recent {
			rows = append(rows, runTimeline(str(receipt, "reviewer_job"), rv, rn, "reviewer", str(receipt, "reviewer"), false)...)
		}
		title := "独立复审要求修改"
		if boolean(receipt, "approved") {
			title = "独立复审通过"
		}
		r := timelineRow(rv, rn, "reviewer", str(receipt, "reviewer"), "done", title, str(receipt, "reviewed_at"))
		detail := str(receipt, "summary")
		linked := map[string]bool{}
		for _, id := range texts(receipt["owner_message_ids"]) {
			if changeVersions[id] == rv {
				linked[id] = true
			}
		}
		if len(linked) > 0 {
			detail = strings.TrimSpace(fmt.Sprintf("本轮复审的候选产物关联了 %d 条修改要求。 %s", len(linked), detail))
		}
		r["detail"] = detail
		rows = append(rows, r)
		if str(receipt, "packaging_error") != "" {
			rows = append(rows, timelineRow(rv, rn, "system", "", "failed", "提交文件组装失败", str(receipt, "reviewed_at")))
		}
	}

	job := str(st, "job")
	reviewJob := str(obj(st, "review_attempt"), "job")
	if job != "" && job != lastReviewed {
		switch status {
		case "working", "reviewing", "failed":
			resumeStatus := str(st, "resume_status")
			complete := optionalRunRecord(job, "complete.json")
			writerActive := status == "working" && len(complete) == 0
			rows = append(rows, runTimeline(job, rev, round, "writer", writer, writerActive)...)
			if len(complete) > 0 {
				r := timelineRow(rev, round, "writer", writer, "done", "候选产物已生成", str(complete, "finished_at"))
				paths := []string{}
				for path := range obj(complete, "hashes") {
					if strings.HasPrefix(filepath.ToSlash(path), "final/") {
						paths = append(paths, path)
					}
				}
				r["files"] = timelineFiles(paths)
				rows = append(rows, r)
			}
			if reviewJob != "" && (status == "reviewing" || resumeStatus == "reviewing") {
				rows = append(rows, runTimeline(reviewJob, rev, round, "reviewer", reviewer, status == "reviewing")...)
			}
		}
	}
	rows = insertOwnerChanges(rows, changes)
	phase := map[string]string{
		"queued": "等待主写开始", "revision_ready": "新版修改已排队", "rewriting": "等待主写按意见返工",
		"delivery_pending": "正在发布本版产物", "awaiting": "本版已交付，等你审阅", "needs_student": "等待本人补充或修改",
		"failed": "作业处理失败", "submitted": "学校已返回提交回执", "submission_unknown": "请核对学校提交结果",
		"submitting": "已确认，正在提交", "approval_invalid": "需要重新确认提交条件",
		"closed_remote": "学堂作业已关闭", "ignored": "已忽略作业",
	}[status]
	if phase != "" {
		state := "waiting"
		if status == "failed" {
			state = "failed"
		}
		if status == "submitted" || status == "closed_remote" || status == "ignored" {
			state = "done"
		}
		if status == "delivery_pending" || status == "submitting" {
			state = "active"
		}
		if status == "revision_ready" {
			rev++
			round = 1
		}
		row := timelineRow(rev, round, "system", "", state, phase, "")
		if status == "submitted" {
			row["at"] = st["submitted_at"]
		}
		rows = append(rows, row)
	}
	return rows
}
