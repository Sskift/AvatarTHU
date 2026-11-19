package app

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Keep a separate original for the reviewer. Already-running legacy jobs have
// only the material bundle that was saved in their input directory.
func reviewMaterials(st M, job string) string {
	if str(obj(st, "review_materials"), "job") == job && job != "" {
		return str(obj(st, "review_materials"), "path")
	}
	return filepath.Join(job, "input")
}

func materialFingerprint(dir string) string {
	hashes := M{}
	for _, file := range visibleFiles(dir) {
		hashes[filepath.ToSlash(relative(dir, file))] = []any{digest(file), artifactMode(file)}
	}
	return fingerprint(hashes)
}

func artifactMode(file string) int {
	info, err := os.Stat(file)
	check(err)
	return int(info.Mode().Perm())
}

func copyArtifact(src, dst string) {
	copyFile(src, dst)
	check(os.Chmod(dst, os.FileMode(artifactMode(src))))
}

func copyArtifactTree(src, dst string) {
	mkdir(dst)
	for _, file := range visibleFiles(src) {
		copyArtifact(file, filepath.Join(dst, relative(src, file)))
	}
}

func candidatePath(rel string) string {
	return "candidate/" + strings.TrimPrefix(filepath.ToSlash(rel), "final/")
}

// Stage exactly the bytes the owner's card will bind. A malformed project
// archive is feedback for the writer; a filesystem failure belongs to the job.
func prepareReviewSubmission(job string, selected []string) (upload, problem string) {
	if len(selected) == 0 {
		return "", ""
	}
	stage := func() {
		if len(selected) == 1 && !strings.EqualFold(filepath.Ext(selected[0]), ".zip") && filepath.Dir(candidatePath(selected[0])) == "candidate" {
			upload = candidatePath(selected[0])
		} else {
			files := make([]string, 0, len(selected))
			for _, rel := range selected {
				files = append(files, inside(job, candidatePath(rel)))
			}
			upload = "upload/submission.zip"
			buildSubmissionZIPFrom(filepath.Join(job, filepath.FromSlash(upload)), files, filepath.Join(job, "candidate"))
		}
		materializeSubmission(inside(job, upload), filepath.Join(job, "submission"))
	}
	if err := attempt(stage); err != nil {
		var fileError *os.PathError
		if errors.As(err, &fileError) {
			panic(err)
		}
		if upload == "upload/submission.zip" {
			if err := os.Remove(filepath.Join(job, filepath.FromSlash(upload))); err != nil && !os.IsNotExist(err) {
				panic(err)
			}
		}
		return "", safeError(err)
	}
	return upload, ""
}

func reviewSchemaFor(files []string) M {
	schema := parseMap(jsonBytes(reviewerSchema))
	names := []string{""}
	for _, rel := range files {
		names = append(names, filepath.Base(rel))
	}
	comments := obj(obj(schema, "properties"), "comments")
	fields := obj(obj(comments, "items"), "properties")
	obj(fields, "artifact")["enum"] = names
	return schema
}

func bindReviewComments(review M, files []string) {
	available := map[string]bool{}
	for _, rel := range files {
		available[filepath.Base(rel)] = true
	}
	for _, comment := range objects(review["comments"]) {
		name := str(comment, "artifact")
		ensure(name == "" || available[name], "复审意见引用了本轮之外的文件："+name)
	}
}

func reviewLocation(comment M) string {
	if name := str(comment, "artifact"); name != "" {
		if where := str(comment, "location"); where != "" {
			return name + " · " + where
		}
		return name
	}
	return str(comment, "location")
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
	if problem := str(entry, "packaging_error"); problem != "" {
		parts = append(parts, "提交文件无法组装："+problem)
	}
	for i, c := range objects(entry["comments"]) {
		parts = append(parts, fmt.Sprintf("%d. 位置：%s\n问题：%s\n修改建议：%s", i+1, reviewLocation(c), str(c, "comment"), str(c, "suggestion")))
	}
	if limits := texts(entry["limitations"]); len(limits) > 0 {
		parts = append(parts, "复审无法确认：\n"+strings.Join(limits, "\n"))
	}
	return strings.Join(parts, "\n\n")
}

func checkReviewArchive(file string) string {
	if !strings.EqualFold(filepath.Ext(file), ".zip") {
		return ""
	}
	z, err := zip.OpenReader(file)
	if errors.Is(err, zip.ErrFormat) {
		return "无法读取 ZIP 文件 " + filepath.Base(file) + "，请重新打包。"
	}
	check(err)
	defer z.Close()
	var nestedBytes int64
	var entries int
	var inspect func([]*zip.File, int) string
	inspect = func(members []*zip.File, depth int) string {
		entries += len(members)
		if entries > 50000 || depth > 8 {
			return "嵌套 ZIP 数量或层级过多，请简化 " + filepath.Base(file)
		}
		for _, member := range members {
			name := filepath.Base(strings.ReplaceAll(member.Name, "\\", "/"))
			if strings.EqualFold(name, "review.md") {
				return "候选压缩包包含主写自查 review.md，不能传给独立复审：" + filepath.Base(file)
			}
			if member.FileInfo().IsDir() || !strings.EqualFold(filepath.Ext(name), ".zip") {
				continue
			}
			budget := (256 << 20) - nestedBytes
			if member.UncompressedSize64 > uint64(budget) {
				return "内嵌 ZIP 超出可检查大小：" + name
			}
			stream, err := member.Open()
			if err != nil {
				return "无法读取内嵌 ZIP：" + name
			}
			data, err := io.ReadAll(io.LimitReader(stream, budget+1))
			check(stream.Close())
			if err != nil || int64(len(data)) > budget {
				return "无法完整读取内嵌 ZIP：" + name
			}
			nestedBytes += int64(len(data))
			child, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
			if err != nil {
				return "无法读取内嵌 ZIP：" + name
			}
			if problem := inspect(child.File, depth+1); problem != "" {
				return problem
			}
		}
		return ""
	}
	return inspect(z.File, 0)
}

func (a *App) review(st, result M, writerJob string, plan M) M {
	files := texts(result["files"])
	unreadable := M{}
	problems := []string{}
	for _, rel := range files {
		if problem := checkReviewArchive(inside(writerJob, rel)); problem != "" {
			unreadable[rel] = problem
			problems = append(problems, problem)
		}
	}
	hashes := hashesFor(writerJob, files)
	legacyKey := fingerprint(hashes)
	selected := files
	if _, explicit := result["submission_files"]; explicit {
		selected = texts(result["submission_files"])
		legacyKey = fingerprint([]any{hashes, selected})
	}
	input := reviewMaterials(st, writerJob)
	inputHash := materialFingerprint(input)
	if material := obj(st, "review_materials"); str(material, "job") == writerJob {
		ensure(inputHash == str(material, "sha256"), "冻结的题目材料已发生变化")
	}
	modes := M{}
	for _, rel := range files {
		modes[rel] = artifactMode(inside(writerJob, rel))
	}
	candidateHash := fingerprint([]any{legacyKey, modes, inputHash})
	matches := func(entry M) bool {
		key := str(entry, "candidate_hash")
		return str(entry, "writer_job") == writerJob && (key == candidateHash || key == legacyKey)
	}
	receipt := M{}
	history := objects(st["review_history"])
	for _, entry := range history {
		if matches(entry) {
			receipt = entry
			break
		}
	}
	cp := obj(st, "review_attempt")
	if len(receipt) == 0 && matches(cp) && str(cp, "job") != "" {
		receipt = readMap(filepath.Join(str(cp, "job"), "complete.json"))
		ensure(len(receipt) == 0 || matches(receipt), "复审回执与当前产物不一致")
	}
	if len(receipt) == 0 {
		job := filepath.Join(filepath.Dir(writerJob), "review-"+randomID(6))
		copyArtifactTree(input, filepath.Join(job, "input"))
		mkdir(filepath.Join(job, "candidate"))
		for _, rel := range files {
			if unreadable[rel] == nil {
				copyArtifact(inside(writerJob, rel), filepath.Join(job, candidatePath(rel)))
			}
		}
		manifest := filepath.Join(job, "submission-files.json")
		upload, problem := "", strings.Join(problems, "\n")
		if problem == "" {
			upload, problem = prepareReviewSubmission(job, selected)
		}
		uploadHash := ""
		if upload != "" {
			uploadHash = digest(inside(job, upload))
		}
		writeJSON(manifest, M{"files": selected, "downloads": files, "unreadable_files": unreadable, "packaging_error": problem})
		manifestHash := digest(manifest)
		st["status"] = "reviewing"
		st["review_attempt"] = M{"job": job, "writer_job": writerJob, "candidate_hash": candidateHash}
		a.saveTask(st)
		value := a.engine(str(plan, "reviewer"), job, reviewerPrompt(job), reviewSchemaFor(files), "reviewer", plan)
		validateReview(value)
		bindReviewComments(value, files)
		ensure(digest(manifest) == manifestHash, "复审修改了提交清单，将重新开启独立复审")
		ensure(materialFingerprint(filepath.Join(job, "input")) == inputHash, "复审修改了题目材料，将重新开启独立复审")
		for _, rel := range files {
			if unreadable[rel] != nil {
				continue
			}
			path := inside(job, candidatePath(rel))
			ensure(digest(path) == hashes[rel] && artifactMode(path) == modes[rel], "复审修改了候选副本，将重新开启独立复审")
		}
		if upload != "" {
			path := inside(job, upload)
			ensure(digest(path) == uploadHash, "复审修改了提交文件，将重新开启独立复审")
			folder := filepath.Join(job, "submission")
			info, err := os.Lstat(folder)
			check(err)
			ensure(info.IsDir(), "复审修改了提交目录，将重新开启独立复审")
			materializeSubmission(path, folder)
		}
		receipt = value
		merge(receipt, M{
			"id": filepath.Base(job), "revision": st["revision"], "round": st["review_round"],
			"writer": plan["writer"], "reviewer": plan["reviewer"], "writer_job": writerJob, "reviewer_job": job,
			"candidate_hash": candidateHash, "artifact_hashes": hashes, "artifact_modes": modes,
			"unreadable_files": unreadable, "submission_files": selected, "reviewed_upload": upload,
			"submission_sha256": uploadHash, "packaging_error": problem,
			"reviewed_at": stamp(), "execution": readMap(filepath.Join(job, "execution.json")),
		})
		if ids := a.conversationJobMessages(st, writerJob); len(ids) > 0 {
			receipt["owner_message_ids"] = ids
		}
		writeJSON(filepath.Join(job, "complete.json"), receipt)
	}
	validateReview(receipt)
	found := false
	for _, h := range history {
		found = found || h["id"] == receipt["id"]
	}
	if !found {
		history = append(history, receipt)
	}
	st["review_history"] = history
	st["review_outcome"] = "changes_requested"
	if boolean(receipt, "approved") && str(receipt, "packaging_error") == "" {
		st["review_outcome"] = "approved"
	}
	a.saveTask(st)
	return receipt
}
