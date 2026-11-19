package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var questionSchema = parseMap([]byte(`{"type":"object","additionalProperties":false,"required":["answer"],"properties":{"answer":{"type":"string"}}}`))

func (a *App) wakeQuestions() {
	select {
	case a.questionWake <- struct{}{}:
	default:
	}
}

func (a *App) questionLoop(ctx context.Context) {
	for ctx.Err() == nil {
		if err := attempt(func() { a.processConversationQuestions("") }); err != nil {
			fmt.Fprintln(os.Stderr, "作业问答："+safeError(err))
		}
		select {
		case <-ctx.Done():
			return
		case <-a.questionWake:
		case <-time.After(time.Minute):
		}
	}
}

func (a *App) processConversationQuestions(selected string) {
	unlock, ok := a.tryLock("conversation-answers")
	if !ok {
		return
	}
	defer unlock()
	for _, st := range a.tasks() {
		if selected != "" && str(st, "task_id") != selected {
			continue
		}
		thread := readMap(a.conversationPath(st))
		for _, turn := range objects(thread["messages"]) {
			if a.Ctx.Err() != nil {
				return
			}
			if str(turn, "role") == "user" && str(turn, "kind") == "ask" && (str(turn, "state") == "queued" || str(turn, "state") == "running") {
				a.answerConversationQuestion(st, str(turn, "id"))
			}
		}
	}
}

func (a *App) answerConversationQuestion(st M, id string) {
	ensure(editorRequestID.MatchString(id), "无效的提问编号")
	var question M
	var history string
	a.updateConversation(st, func(thread M) {
		messages := objects(thread["messages"])
		for i, turn := range messages {
			if str(turn, "id") == id && (str(turn, "state") == "queued" || str(turn, "state") == "running") {
				turn["state"] = "running"
				question = turn
				history = questionHistory(messages, i)
				return
			}
		}
	})
	if question == nil {
		return
	}
	st = readMap(a.taskPath(str(st, "task_id")))
	plan := M{}
	merge(plan, obj(question, "plan"))
	plan["read_only"] = true
	plan["stage_timeout"] = min(number(plan, "stage_timeout", 7200), 600)
	writer := str(question, "writer")
	root := filepath.Join(filepath.Dir(a.conversationPath(st)), "questions", id)
	receipt := filepath.Join(root, "answer.json")
	var answer string
	err := attempt(func() {
		if saved := readMap(receipt); str(saved, "answer") != "" {
			answer = str(saved, "answer")
			return
		}
		job := filepath.Join(root, "attempt-"+randomID(5))
		mkdir(job)
		// The thread and its answer are durable. Large read-only copies only
		// need to exist while the harness reads them.
		defer os.RemoveAll(filepath.Join(job, "materials"))
		defer os.RemoveAll(filepath.Join(job, "files"))
		defer os.RemoveAll(filepath.Join(job, "unpacked"))
		info := a.questionInputs(st, question, job)
		writeJSON(filepath.Join(job, "assignment.json"), info)
		prompt := "你是此作业的主写。本人在工作台提出了只读问题。用中文直接作答，必要时引用版本、文件和位置。\n" +
			"assignment.json 是已记录的进度与交付信息，materials/ 是题目资料，files/ 是问题所指版本的冻结文件，unpacked/ 提供可读取的压缩包内容。只读取本目录；说明证据中尚不能确定的内容。不得修改产物、执行作业程序、访问外部账号或提交作业。用户若希望应用修改，可以提出具体建议，说明可用“调整产物”交给主写。\n" +
			"以下是截至这次提问的对话：\n" + history + "\n本次问题：\n" + str(question, "text")
		value := a.runHarness(writer, job, prompt, questionSchema, "question", plan)
		text, ok := value["answer"].(string)
		answer = strings.TrimSpace(text)
		ensure(ok && answer != "" && len(answer) <= 128<<10, "主写没有返回有效答复")
		writeJSON(receipt, M{"answer": answer, "writer": writer, "answered_at": stamp()})
	})
	a.updateConversation(st, func(thread M) {
		for _, m := range objects(thread["messages"]) {
			if str(m, "id") != id || str(m, "state") == "answered" {
				continue
			}
			if err == nil {
				conversationReply(thread, []string{id}, answer, writer, number(question, "revision", 1), 0)
			} else if errors.Is(err, context.Canceled) || a.Ctx.Err() != nil {
				m["state"] = "queued"
			} else {
				m["state"] = "error"
				m["error"] = safeError(err)
				var failure *toolFailure
				if errors.As(err, &failure) {
					m["error"] = failure.Reason + "。" + failure.Action
				}
			}
			return
		}
	})
}

// A preceding question may have been answered after this one was queued.
// Include its reply, but do not let a later request change this question.
func questionHistory(messages []M, index int) string {
	prior := append([]M{}, messages[:index]...)
	owners := map[string]bool{}
	for _, m := range prior {
		if str(m, "role") == "user" {
			owners[str(m, "id")] = true
		}
	}
	for _, m := range messages[index+1:] {
		parents := texts(m["reply_to"])
		eligible := str(m, "role") == "writer" && len(parents) > 0
		for _, parent := range parents {
			eligible = eligible && owners[parent]
		}
		if eligible {
			prior = append(prior, m)
		}
	}
	return conversationContext(prior)
}

func (a *App) questionInputs(st, question M, job string) M {
	rev := number(question, "revision", 1)
	fileRevision := number(question, "file_revision", 0)
	versions := append(objects(st["revision_history"]), st)
	var delivery, materials M
	for _, version := range versions {
		if fileRevision > 0 && publishedRevision(version) == fileRevision && !boolean(version, "artifacts_removed") {
			delivery = version
		}
		if record := obj(version, "review_materials"); number(record, "revision", 0) == rev {
			materials = record
		}
	}
	info := M{"title": st["title"], "course": st["course"], "asked_about_revision": rev, "file_revision": fileRevision, "current_revision": st["revision"], "status": st["status"], "observed_at": stamp(), "timeline": a.workspaceTimeline(st, nil)}
	dest := filepath.Join(job, "materials")
	if src := str(materials, "path"); src != "" && exists(filepath.Join(src, "assignment.md")) {
		copyArtifactTree(src, dest)
		ensure(materialFingerprint(dest) == str(materials, "sha256"), "本版原题快照已变化，无法据此回答")
		info["materials_source"] = "该版本的原题快照"
	} else {
		func() {
			defer a.lock("course-materials", true)()
			freezeMaterials(st, dest)
		}()
		info["materials_source"] = "当前本机题目与资料"
	}
	if fileRevision == 0 {
		return info
	}
	ensure(len(delivery) > 0, "提问所指的交付版本已不可用")
	names := texts(question["files"])
	selected := map[string]bool{}
	for _, name := range names {
		selected[name] = true
	}
	mkdir(filepath.Join(job, "files"))
	available := []string{}
	unreadable := []string{}
	for _, artifact := range objects(delivery["artifacts"]) {
		file := str(artifact, "path")
		name := filepath.Base(file)
		available = append(available, name)
		if len(names) > 0 && !selected[name] {
			continue
		}
		base := strDefault(delivery, "output_dir", filepath.Join(a.Root, "outbox", str(st, "task_id"), fmt.Sprintf("r%d", fileRevision)))
		src := inside(base, relative(base, file))
		dst := filepath.Join(job, "files", name)
		copyArtifact(src, dst)
		ensure(digest(dst) == str(artifact, "sha256"), "提问所指文件与冻结版本不一致："+name)
		delete(selected, name)
		if strings.EqualFold(filepath.Ext(name), ".zip") {
			if err := attempt(func() { materializeSubmission(dst, filepath.Join(job, "unpacked", name)) }); err != nil {
				unreadable = append(unreadable, name)
			}
		}
	}
	ensure(len(selected) == 0, "提问所选文件已不可用")
	info["available_files"] = available
	info["selected_files"] = names
	info["unreadable_archives"] = unreadable
	return info
}
