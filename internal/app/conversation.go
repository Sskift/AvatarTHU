package app

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

type conversationRequest struct {
	ID           string   `json:"id"`
	Kind         string   `json:"kind"`
	Revision     int      `json:"revision"`
	FileRevision int      `json:"file_revision"`
	Instruction  string   `json:"instruction"`
	Files        []string `json:"files"`
}

func (a *App) conversationPath(st M) string {
	base := strDefault(st, "assignment_dir", a.data("jobs", str(st, "task_id")))
	return filepath.Join(assignmentRoot(base), "workspace", "conversation.json")
}

// The scheduler owns the task lock throughout model execution. A separate,
// short-lived lock lets the owner leave instructions while either agent works.
func (a *App) updateConversation(st M, change func(M)) {
	defer a.lock("conversation-"+str(st, "task_id"), true)()
	path := a.conversationPath(st)
	thread := readMap(path)
	change(thread)
	if len(thread) > 0 {
		writeJSON(path, thread)
	}
}

func awaitsOwnerChange(st M) bool {
	return editorEditable(st) || str(st, "status") == "revision_requested"
}

func canSteer(st M) bool {
	switch str(st, "status") {
	case "queued", "working", "reviewing", "rewriting", "revision_ready", "delivery_pending", "failed":
		return true
	default:
		return awaitsOwnerChange(st)
	}
}

func conversationFileRevision(st M) int {
	if boolean(st, "artifacts_removed") || len(objects(st["artifacts"])) == 0 {
		return 0
	}
	return publishedRevision(st)
}

func (a *App) conversationView(st M) M {
	rows := []M{}
	for _, m := range objects(readMap(a.conversationPath(st))["messages"]) {
		row := M{}
		for _, key := range []string{"id", "role", "kind", "text", "revision", "target_revision", "round", "files", "file_revision", "state", "created_at", "writer", "reply_to", "error"} {
			if v, ok := m[key]; ok {
				row[key] = v
			}
		}
		if str(m, "kind") == "change" && str(m, "role") == "user" && (str(m, "state") == "queued" || str(m, "state") == "running") {
			switch str(st, "status") {
			case "failed":
				row["state"], row["error"] = "waiting", strDefault(st, "error", "等待恢复作业处理")
			case "ignored":
				row["state"], row["error"] = "waiting", "作业已忽略，恢复后可继续处理"
			case "closed_remote", "submitted", "submission_unknown", "submitting":
				row["state"], row["error"] = "stopped", "作业当前状态已停止修改"
			}
		}
		rows = append(rows, row)
	}
	return M{"can_send": canSteer(st), "can_ask": true, "file_revision": conversationFileRevision(st), "messages": rows}
}

func (a *App) sendConversation(tid string, in conversationRequest) {
	taskPath := a.taskPath(tid)
	ensure(editorRequestID.MatchString(in.ID), "无效的对话消息编号")
	ensure(in.Kind == "change" || in.Kind == "ask", "请选择提问或调整产物")
	in.Instruction = strings.TrimSpace(in.Instruction)
	ensure(in.Instruction != "" && len(in.Instruction) <= 12000, "请填写消息（最多 12 KB）")
	unlock, taskOwned := a.tryLock(tid)
	if taskOwned {
		defer unlock()
	}
	st := readMap(taskPath)
	ensure(len(st) > 0, "找不到这份作业")
	a.updateConversation(st, func(thread M) {
		msgs := objects(thread["messages"])
		for _, m := range msgs {
			if str(m, "id") == in.ID {
				ensure(str(m, "role") == "user" && str(m, "kind") == in.Kind && str(m, "text") == in.Instruction &&
					number(m, "revision", 0) == in.Revision && slices.Equal(texts(m["files"]), in.Files) &&
					(len(in.Files) == 0 || number(m, "file_revision", 0) == in.FileRevision), "这个消息编号已被其他内容使用")
				return
			}
		}
		// Check again under the same lock used to seal a version for publication.
		st = readMap(taskPath)
		revision := number(st, "revision", 1)
		ensure(in.Revision == revision, "作业版本已更新，请核对当前版本后再发送")
		target := revision
		if in.Kind == "change" {
			ensure(canSteer(st), "当前状态不能修改产物")
			if awaitsOwnerChange(st) || str(st, "status") == "revision_ready" || str(st, "status") == "delivery_pending" ||
				number(st, "conversation_sealed_revision", 0) == revision || (str(st, "status") == "reviewing" && reviewLimitReached(st)) {
				target++
			}
		}
		files := objects(st["artifacts"])
		fileRevision := conversationFileRevision(st)
		if len(in.Files) > 0 {
			ensure(fileRevision > 0 && in.FileRevision == fileRevision, "所选文件的版本已更新，请重新选择")
			available := map[string]bool{}
			for _, f := range files {
				available[filepath.Base(str(f, "path"))] = true
			}
			for _, name := range in.Files {
				ensure(available[name], "所选文件不属于本版产物或重复")
				delete(available, name)
			}
		}
		plan := obj(st, "execution_plan")
		if len(plan) == 0 || target != revision || str(st, "status") == "revision_ready" {
			plan = a.pairing()
		}
		thread["messages"] = append(msgs, M{"id": in.ID, "kind": in.Kind, "role": "user", "text": in.Instruction, "revision": revision,
			"target_revision": target, "file_revision": fileRevision, "files": in.Files, "writer": plan["writer"], "plan": plan, "state": "queued", "created_at": stamp()})
	})
	if in.Kind == "ask" {
		a.wakeQuestions()
	} else {
		if taskOwned {
			a.queuePendingConversation(st)
		}
		a.wakeEditor()
	}
}

// Caller holds the task lock. This also recovers a crash between durable
// acceptance of the message and invalidation of the previous card.
func (a *App) queuePendingConversation(st M) {
	if !awaitsOwnerChange(st) {
		return
	}
	feedback := []string{}
	for _, m := range objects(readMap(a.conversationPath(st))["messages"]) {
		if str(m, "role") == "user" && str(m, "kind") == "change" && str(m, "state") == "queued" {
			feedback = append(feedback, conversationLine(m))
		}
	}
	if len(feedback) > 0 {
		a.queueRevision(st, strings.Join(feedback, "\n"))
	}
}

func conversationLine(m M) string {
	who := "用户要求修改"
	if str(m, "role") == "writer" {
		who = "主写回复"
	} else if str(m, "kind") == "ask" {
		who = "用户提问"
	}
	files := ""
	if names := texts(m["files"]); len(names) > 0 {
		files = fmt.Sprintf("（第 %d 版 %s）", number(m, "file_revision", 0), strings.Join(names, "、"))
	}
	return who + files + "：" + str(m, "text")
}

func conversationContext(messages []M) string {
	const limit = 48 << 10
	start, size := len(messages), 0
	for start > 0 {
		n := len(conversationLine(messages[start-1])) + 1
		if start < len(messages) && size+n > limit {
			break
		}
		size += n
		start--
	}
	lines := []string{}
	for _, m := range messages[start:] {
		lines = append(lines, conversationLine(m))
	}
	return strings.Join(lines, "\n")
}

func conversationReply(thread M, ids []string, text, writer string, revision, round int) {
	messages := objects(thread["messages"])
	kind := ""
	matched := []string{}
	for _, m := range messages {
		if str(m, "role") != "user" || str(m, "state") == "answered" || !slices.Contains(ids, str(m, "id")) {
			continue
		}
		m["state"] = "answered"
		kind = str(m, "kind")
		matched = append(matched, str(m, "id"))
	}
	if len(matched) == 0 {
		return
	}
	thread["messages"] = append(messages, M{"id": randomID(16), "role": "writer", "kind": kind, "state": "answered", "text": text,
		"revision": revision, "target_revision": revision, "round": round, "writer": writer, "reply_to": matched, "created_at": stamp()})
}

// Bind a message only to a fresh job, never to a result restored from a
// checkpoint. A replacement process can reclaim interrupted, unanswered turns.
func (a *App) writerConversation(st M, job string, fresh bool) string {
	revision := number(st, "revision", 1)
	prompt := ""
	a.updateConversation(st, func(thread M) {
		messages := objects(thread["messages"])
		requests := []string{}
		history := []M{}
		for _, m := range messages {
			if number(m, "target_revision", revision) > revision {
				continue
			}
			history = append(history, m)
			if str(m, "role") != "user" || str(m, "kind") != "change" {
				continue
			}
			state := str(m, "state")
			if fresh && (state == "queued" || state == "running") {
				merge(m, M{"state": "running", "job": job, "target_revision": revision, "round": st["review_round"], "writer": obj(st, "execution_plan")["writer"]})
			}
			if number(m, "target_revision", 0) == revision && (str(m, "job") == job || state == "answered") {
				requests = append(requests, conversationLine(m))
			}
		}
		if len(requests) > 0 {
			prompt = "\n作业对话（提问和主写回复只作上下文）：\n" + conversationContext(history) + "\n本版需要落实的用户消息：\n" + strings.Join(requests, "\n") +
				"\n最新的调整优先于较早的措辞要求。更新实际文件并验证，在 owner_reply 直接答复用户改了哪些文件、如何验证或哪些内容仍需确认。对话只供主写，不要复制进报告或复审输入。"
		}
	})
	return prompt
}

func (a *App) answerConversation(st M, job string, result M) {
	a.updateConversation(st, func(thread M) {
		ids := []string{}
		for _, m := range objects(thread["messages"]) {
			if str(m, "kind") == "change" && str(m, "role") == "user" && str(m, "job") == job && str(m, "state") == "running" {
				ids = append(ids, str(m, "id"))
			}
		}
		if len(ids) == 0 {
			return
		}
		reply := strings.TrimSpace(str(result, "owner_reply"))
		if reply == "" {
			reply = str(result, "summary")
			for _, note := range objects(obj(result, "presentation")["revision_notes"]) {
				reply += "\n" + str(note, "detail")
			}
		}
		conversationReply(thread, ids, reply, str(obj(st, "execution_plan"), "writer"), number(st, "revision", 1), number(st, "review_round", 1))
	})
}

func reviewLimitReached(st M) bool {
	maximum := number(obj(st, "execution_plan"), "max_review_rounds", 3)
	return maximum > 0 && number(st, "review_round", 1) >= maximum
}

func (a *App) conversationJobMessages(st M, job string) []string {
	var ids []string
	for _, m := range objects(readMap(a.conversationPath(st))["messages"]) {
		if str(m, "kind") == "change" && str(m, "role") == "user" && str(m, "job") == job {
			ids = append(ids, str(m, "id"))
		}
	}
	return ids
}

// The same lock serializes incoming instructions with the final decision to
// freeze. A later message targets a new revision and cannot change these bytes.
func (a *App) conversationCheckpoint(st M, job string, reviewed bool) bool {
	defer a.lock("conversation-"+str(st, "task_id"), true)()
	revision := number(st, "revision", 1)
	path := a.conversationPath(st)
	thread := readMap(path)
	deferred := false
	for _, m := range objects(thread["messages"]) {
		if str(m, "role") == "user" && str(m, "kind") == "change" && str(m, "state") == "queued" && number(m, "target_revision", revision) <= revision {
			if reviewed && reviewLimitReached(st) {
				m["target_revision"] = revision + 1
				deferred = true
				continue
			}
			if reviewed {
				st["review_round"] = number(st, "review_round", 1) + 1
			}
			merge(st, M{"previous_job": job, "conversation_previous_job": job, "status": "rewriting"})
			delete(st, "job")
			delete(st, "review_attempt")
			a.saveTask(st)
			return true
		}
	}
	if deferred {
		writeJSON(path, thread)
	}
	if reviewed && number(st, "conversation_sealed_revision", 0) != revision {
		st["conversation_sealed_revision"] = revision
		a.saveTask(st)
	}
	return false
}
