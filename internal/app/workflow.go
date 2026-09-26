package app

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/ledongthuc/pdf"
)

func materialsHash(meta M) string {
	paths := []any{}
	for _, base := range []string{str(meta, "folder"), str(meta, "courseware")} {
		for _, p := range visibleFiles(base) {
			switch filepath.Base(p) {
			case "answer.md", "answer.final.md", "review.md":
				continue
			}
			paths = append(paths, []any{filepath.Base(base), filepath.ToSlash(relative(base, p)), digest(p)})
		}
	}
	return fingerprint([]any{meta["description"], meta["deadline"], meta["requirements_hash"], paths})
}
func extractDocument(src, dst string) {
	switch strings.ToLower(filepath.Ext(src)) {
	case ".pdf":
		f, r, e := pdf.Open(src)
		check(e)
		defer f.Close()
		reader, e := r.GetPlainText()
		check(e)
		text, e := io.ReadAll(io.LimitReader(reader, 16<<20))
		check(e)
		if len(strings.TrimSpace(string(text))) == 0 {
			text = []byte("[此 PDF 没有可提取文字，必须查看原文件中的扫描页和图示。]")
		}
		writeFile(dst, text, 0600)
	case ".docx", ".pptx":
		z, e := zip.OpenReader(src)
		check(e)
		defer z.Close()
		names := map[string]*zip.File{}
		keys := []string{}
		for _, f := range z.File {
			if (f.Name == "word/document.xml" || strings.HasPrefix(f.Name, "ppt/slides/slide")) && strings.HasSuffix(f.Name, ".xml") {
				names[f.Name] = f
				keys = append(keys, f.Name)
			}
		}
		sort.Strings(keys)
		var text strings.Builder
		for _, n := range keys {
			f := names[n]
			ensure(f.UncompressedSize64 < 32<<20, "Office 文档 XML 太大")
			r, e := f.Open()
			check(e)
			d := xml.NewDecoder(io.LimitReader(r, 32<<20))
			for {
				t, e := d.Token()
				if e == io.EOF {
					break
				}
				check(e)
				if start, ok := t.(xml.StartElement); ok && start.Name.Local == "t" {
					var s string
					check(d.DecodeElement(&s, &start))
					text.WriteString(s + " ")
				}
			}
			r.Close()
			text.WriteString("\n\n")
		}
		writeFile(dst, []byte(text.String()), 0600)
	}
}
func (a *App) prepare(st M, job string) {
	writeFile(filepath.Join(job, "input", "assignment.md"), []byte(str(st, "description")), 0600)
	for name, source := range map[string]string{"attachments": str(st, "folder"), "courseware": str(st, "courseware")} {
		dest := filepath.Join(job, "input", name)
		mkdir(dest)
		for _, p := range visibleFiles(source) {
			switch filepath.Base(p) {
			case "README.md", "answer.md", "answer.final.md", "review.md":
				continue
			}
			target := filepath.Join(dest, relative(source, p))
			copyFile(p, target)
			ext := strings.ToLower(filepath.Ext(p))
			if ext == ".pdf" || ext == ".docx" || ext == ".pptx" {
				if e := attempt(func() { extractDocument(p, target+".txt") }); e != nil {
					writeFile(target+".txt", []byte("[自动文本提取未完成，须查看原始附件；不能把此提示当作题目内容。]\n"+safeError(e)), 0600)
				}
			}
		}
	}
	if previous := str(st, "previous_job"); previous != "" {
		copyTree(filepath.Join(previous, "final"), filepath.Join(job, "previous-final"))
	} else if exists(filepath.Join(str(st, "folder"), "answer.md")) {
		copyFile(filepath.Join(str(st, "folder"), "answer.md"), filepath.Join(job, "previous-draft", "answer.md"))
	}
	writeJSON(filepath.Join(job, "output-schema.json"), writerSchema)
}
func validateWriter(r M, job string) M {
	_, ok := r["ready"].(bool)
	ensure(ok, "主写未返回 ready 布尔值")
	_, ok = r["summary"].(string)
	ensure(ok, "主写未返回摘要")
	ensure(r["files"] != nil && r["blockers"] != nil, "主写未返回产物或阻塞事项列表")
	files := texts(r["files"])
	// CLIs sometimes report names relative to final/ despite having created the
	// right files. Resolve that unambiguous form within final/, never outside it.
	for i, p := range files {
		if !strings.HasPrefix(filepath.ToSlash(p), "final/") {
			inside(filepath.Join(job, "final"), p)
			files[i] = "final/" + filepath.ToSlash(p)
			for _, point := range objects(obj(r, "presentation")["highlights"]) {
				if str(point, "artifact") == p {
					point["artifact"] = files[i]
				}
			}
		}
	}
	r["files"] = files
	blockers := texts(r["blockers"])
	ensure(!boolean(r, "ready") || len(files) > 0, "没有交付产物")
	seen := map[string]bool{}
	names := map[string]bool{}
	for _, p := range files {
		ensure(strings.HasPrefix(filepath.ToSlash(p), "final/") && !seen[p], "产物必须位于 final/ 且不能重复")
		f := inside(job, p)
		name := filepath.Base(f)
		ensure(name != "review.md" && name != "submission.zip" && !names[name], "产物文件名重名或保留名称")
		seen[p] = true
		names[name] = true
	}
	inside(job, "review.md")
	if len(blockers) > 0 {
		r["ready"] = false
	}
	if pres := obj(r, "presentation"); len(pres) > 0 {
		notes := objects(pres["revision_notes"])
		ensure(len(notes) <= 30, "逐条修改说明超过 30 项")
		for _, note := range notes {
			status := str(note, "status")
			ensure(status == "addressed" || status == "partial" || status == "unresolved", "修改说明状态无效")
			ensure(len([]rune(str(note, "request"))) <= 500 && len([]rune(str(note, "detail"))) <= 2000, "修改说明过长")
			for _, file := range texts(note["files"]) {
				path := filepath.ToSlash(file)
				if !strings.HasPrefix(path, "final/") {
					path = "final/" + path
				}
				ensure(seen[path], "修改说明必须引用当前交付文件")
			}
		}
		ensure(len([]rune(str(pres, "assignment"))) <= 4000, "题意摘要过长")
		ensure(len(texts(pres["checks"])) <= 8, "检查摘要过多")
		points := objects(pres["highlights"])
		ensure(len(points) <= 3, "关键图超过三张")
		for _, point := range points {
			ensure(seen[str(point, "artifact")], "关键图必须引用当前交付文件")
			imageData(inside(job, str(point, "artifact")), str(point, "member"))
		}
	}
	return r
}
func hashesFor(job string, files []string) M {
	m := M{}
	for _, p := range files {
		m[p] = digest(inside(job, p))
	}
	return m
}
func (a *App) writer(job, prompt string, plan M) M {
	completed := filepath.Join(job, "complete.json")
	if exists(completed) {
		r := validateWriter(readMap(filepath.Join(job, "result.json")), job)
		expected := obj(readMap(completed), "hashes")
		actual := hashesFor(job, append(texts(r["files"]), "review.md"))
		ensure(reflect.DeepEqual(expected, actual), "已完成产物发生变化，需要创建新版本")
		return r
	}
	var raw M
	if done := readMap(filepath.Join(job, "execution.json")); str(done, "role") == "writer" && str(done, "completed_at") != "" {
		if str(done, "executor") == "claude" {
			raw = obj(readMap(filepath.Join(job, "claude.json")), "structured_output")
		} else {
			raw = readMap(filepath.Join(job, "engine-output.json"))
		}
	} else {
		raw = a.engine(str(plan, "writer"), job, prompt, writerSchema, "writer", plan)
	}
	r := validateWriter(raw, job)
	writeJSON(filepath.Join(job, "result.json"), r)
	writeJSON(completed, M{"finished_at": stamp(), "hashes": hashesFor(job, append(texts(r["files"]), "review.md"))})
	return r
}
func validateReview(r M) {
	_, ok := r["approved"].(bool)
	ensure(ok, "复审未返回明确结论")
	_, ok = r["summary"].(string)
	ensure(ok, "复审摘要缺失")
	ensure(r["comments"] != nil && r["checks"] != nil && r["limitations"] != nil, "复审意见或检查记录缺失")
	for _, m := range objects(r["comments"]) {
		for _, k := range []string{"location", "comment", "suggestion"} {
			_, ok := m[k].(string)
			ensure(ok, "复审意见缺少 "+k)
		}
	}
	texts(r["checks"])
	texts(r["limitations"])
}
func reviewFeedback(entry M) string {
	parts := []string{str(entry, "summary")}
	for i, c := range objects(entry["comments"]) {
		parts = append(parts, fmt.Sprintf("%d. 位置：%s\n问题：%s\n修改建议：%s", i+1, str(c, "location"), str(c, "comment"), str(c, "suggestion")))
	}
	if limits := texts(entry["limitations"]); len(limits) > 0 {
		parts = append(parts, "复审无法确认：\n"+strings.Join(limits, "\n"))
	}
	return strings.Join(parts, "\n\n")
}
func (a *App) review(st, result M, writerJob string, plan M) M {
	files := texts(result["files"])
	hashes := hashesFor(writerJob, files)
	candidateHash := fingerprint(hashes)
	for _, entry := range objects(st["review_history"]) {
		if str(entry, "writer_job") == writerJob && str(entry, "candidate_hash") == candidateHash {
			return entry
		}
	}
	cp := obj(st, "review_attempt")
	receipt := M{}
	if str(cp, "writer_job") == writerJob && str(cp, "candidate_hash") == candidateHash && str(cp, "job") != "" {
		receipt = readMap(filepath.Join(str(cp, "job"), "complete.json"))
	}
	if len(receipt) == 0 {
		job := filepath.Join(filepath.Dir(writerJob), "review-"+randomID(6))
		copyTree(filepath.Join(writerJob, "input"), filepath.Join(job, "input"))
		mkdir(filepath.Join(job, "candidate"))
		for _, rel := range files {
			copyFile(inside(writerJob, rel), filepath.Join(job, "candidate", strings.TrimPrefix(filepath.ToSlash(rel), "final/")))
		}
		st["status"] = "reviewing"
		st["review_attempt"] = M{"job": job, "writer_job": writerJob, "candidate_hash": candidateHash}
		a.saveTask(st)
		value := a.engine(str(plan, "reviewer"), job, reviewerPrompt(job), reviewerSchema, "reviewer", plan)
		validateReview(value)
		after := M{}
		for _, rel := range files {
			after[rel] = digest(inside(filepath.Join(job, "candidate"), strings.TrimPrefix(filepath.ToSlash(rel), "final/")))
		}
		ensure(reflect.DeepEqual(after, hashes), "复审修改了候选副本，将重新开启独立复审")
		receipt = value
		merge(receipt, M{"id": filepath.Base(job), "revision": st["revision"], "round": st["review_round"], "writer": plan["writer"], "reviewer": plan["reviewer"], "writer_job": writerJob, "reviewer_job": job, "candidate_hash": candidateHash, "reviewed_at": stamp(), "execution": readMap(filepath.Join(job, "execution.json"))})
		writeJSON(filepath.Join(job, "complete.json"), receipt)
	}
	validateReview(receipt)
	history := objects(st["review_history"])
	found := false
	for _, h := range history {
		found = found || h["id"] == receipt["id"]
	}
	if !found {
		history = append(history, receipt)
	}
	st["review_history"] = history
	st["review_outcome"] = "changes_requested"
	if boolean(receipt, "approved") {
		st["review_outcome"] = "approved"
	}
	a.saveTask(st)
	return receipt
}
func (a *App) process(st M) {
	switch str(st, "status") {
	case "delivery_pending":
		a.deliver(st)
		return
	case "awaiting", "needs_student", "submitted", "submission_unknown", "submitting", "closed_remote", "approval_invalid":
		return
	case "revision_ready":
		a.rememberRevision(st)
		st["previous_job"] = st["job"]
		st["revision"] = number(st, "revision", 0) + 1
		for _, k := range []string{"job", "review_attempt", "execution_plan", "review_round", "review_feedback", "review_outcome"} {
			delete(st, k)
		}
		st["status"] = "queued"
	}
	if st["revision"] == nil {
		st["revision"] = 1
	}
	if st["review_round"] == nil {
		st["review_round"] = 1
	}
	plan := obj(st, "execution_plan")
	if len(plan) == 0 {
		plan = a.pairing()
		st["execution_plan"] = plan
	}
	a.saveTask(st)
	a.requireUnblockedTools(plan)
	a.requirePair(plan)
	var result M
	var job string
	for {
		check(a.Ctx.Err())
		job = str(st, "job")
		if job == "" || (!exists(filepath.Join(job, "complete.json")) && str(readMap(filepath.Join(job, "execution.json")), "completed_at") == "") {
			base := strDefault(st, "assignment_dir", a.data("jobs", str(st, "task_id")))
			job = filepath.Join(base, "runs", fmt.Sprintf("r%d", number(st, "revision", 1)), fmt.Sprintf("round-%d", number(st, "review_round", 1)), "writer-"+randomID(5))
			mkdir(job)
			a.prepare(st, job)
			writeJSON(filepath.Join(job, "execution-plan.json"), plan)
		}
		merge(st, M{"job": job, "status": "working"})
		a.saveTask(st)
		result = a.writer(job, writerPrompt(st, job, str(plan, "writer"), strDefault(st, "review_feedback", "无")), plan)
		decision := a.review(st, result, job, plan)
		if boolean(decision, "approved") {
			break
		}
		maximum := number(plan, "max_review_rounds", 3)
		if maximum > 0 && number(st, "review_round", 1) >= maximum {
			result["ready"] = false
			result["blockers"] = append(texts(result["blockers"]), fmt.Sprintf("第 %d 轮独立复审仍未通过，请查看文档末尾的意见后提出修改要求", number(st, "review_round", 1)))
			break
		}
		merge(st, M{"previous_job": job, "review_round": number(st, "review_round", 1) + 1, "review_feedback": reviewFeedback(decision), "status": "rewriting"})
		delete(st, "job")
		delete(st, "review_attempt")
		a.saveTask(st)
	}
	if str(st, "deadline") == "未提供" {
		result["ready"] = false
		result["blockers"] = append(texts(result["blockers"]), "未提供截止时间，需要本人核对是否可提交")
	}
	a.snapshot(st, result, job)
	a.deliver(st)
}
func (a *App) outputDir(st M) string {
	if d := str(st, "output_dir"); d != "" {
		return d
	}
	return filepath.Join(a.Root, "outbox", str(st, "task_id"), fmt.Sprintf("r%d", number(st, "revision", 1)))
}
func (a *App) snapshot(st, r M, job string) {
	review := M{}
	for _, key := range []string{"document_id", "url"} {
		if value := str(obj(st, "review_doc"), key); value != "" {
			review[key] = value
		}
	}
	base := filepath.Join(a.Root, "outbox", str(st, "task_id"))
	if d := str(st, "assignment_dir"); d != "" {
		base = filepath.Join(d, "outputs")
	}
	target := filepath.Join(base, fmt.Sprintf("r%d", number(st, "revision", 1)))
	mkdir(target)
	files := []string{}
	artifacts := []M{}
	for _, rel := range texts(r["files"]) {
		src := inside(job, rel)
		dst := filepath.Join(target, filepath.Base(src))
		copyFile(src, dst)
		files = append(files, dst)
		artifacts = append(artifacts, M{"path": dst, "sha256": digest(dst)})
	}
	report := filepath.Join(target, "review.md")
	copyFile(inside(job, "review.md"), report)
	var submission, hash any
	if len(files) == 1 {
		submission = files[0]
		hash = digest(files[0])
	} else if len(files) > 1 {
		bundle := filepath.Join(target, "submission.zip")
		f, e := os.OpenFile(bundle, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
		check(e)
		defer f.Close()
		z := zip.NewWriter(f)
		for _, p := range files {
			entry, e := z.Create(filepath.Base(p))
			check(e)
			src, e := os.Open(p)
			check(e)
			_, e = io.Copy(entry, src)
			src.Close()
			check(e)
		}
		check(z.Close())
		check(f.Sync())
		check(f.Close())
		submission = bundle
		hash = digest(bundle)
	}
	merge(st, M{"output_dir": target, "ready": r["ready"], "summary": r["summary"], "blockers": r["blockers"], "artifacts": artifacts, "report": report, "report_sha256": digest(report), "presentation": obj(r, "presentation"), "submission": submission, "sha256": hash, "nonce": randomID(16), "deliveries": M{}, "review_doc": review, "links_synced": false, "links_retry_at": nil, "status": "delivery_pending"})
	for _, k := range []string{"card_message_id", "chat_id", "local_review", "delivery_mode"} {
		delete(st, k)
	}
	a.saveTask(st)
}
