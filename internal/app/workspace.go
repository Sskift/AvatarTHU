package app

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type workspaceCourse struct{ ID, Name, Semester, Dir string }

func (a *App) workspaceCourses() []workspaceCourse {
	result := []workspaceCourse{}
	seen := map[string]bool{}
	for _, root := range []string{filepath.Join(a.Root, "courses"), a.data("courses")} {
		paths, _ := filepath.Glob(filepath.Join(root, "*", "*", "course.json"))
		for _, p := range paths {
			dir, err := filepath.EvalSymlinks(filepath.Dir(p))
			if err != nil || seen[dir] {
				continue
			}
			seen[dir] = true
			m := readMap(p)
			name := strDefault(m, "kcm", filepath.Base(dir))
			semester := strDefault(m, "xnxq", filepath.Base(filepath.Dir(dir)))
			result = append(result, workspaceCourse{fingerprint([]any{semester, str(m, "wlkcid"), dir})[:16], name, semester, dir})
		}
	}
	for _, st := range a.tasks() {
		ware := str(st, "courseware")
		if ware == "" {
			continue
		}
		dir := filepath.Dir(ware)
		if canonical, err := filepath.EvalSymlinks(dir); err == nil {
			dir = canonical
		}
		if seen[dir] {
			continue
		}
		seen[dir] = true
		result = append(result, workspaceCourse{fingerprint([]any{st["semester"], st["course_id"], dir})[:16], str(st, "course"), str(st, "semester"), dir})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Semester != result[j].Semester {
			return result[i].Semester > result[j].Semester
		}
		return result[i].Name < result[j].Name
	})
	return result
}
func (a *App) workspaceCourse(id string) workspaceCourse {
	for _, c := range a.workspaceCourses() {
		if c.ID == id {
			return c
		}
	}
	panic(fmt.Errorf("找不到这门课程"))
}
func courseContainsTask(c workspaceCourse, st M) bool {
	dir := filepath.Dir(str(st, "courseware"))
	if canonical, e := filepath.EvalSymlinks(dir); e == nil {
		dir = canonical
	}
	return dir == c.Dir
}
func publicCourseFile(rel string) string {
	if strings.ContainsAny(rel, "\\:") || filepath.IsAbs(rel) {
		return ""
	}
	p := strings.Split(filepath.ToSlash(rel), "/")
	for _, part := range p {
		if part == "" || strings.HasPrefix(part, ".") {
			return ""
		}
	}
	if len(p) >= 2 && p[0] == "courseware" && p[len(p)-1] != "index.json" {
		return "courseware"
	}
	if len(p) == 2 && p[0] == "notices" && strings.HasSuffix(p[1], ".md") {
		return "notice"
	}
	if len(p) >= 4 && p[0] == "homework" {
		switch p[2] {
		case "source":
			return "assignment"
		case "report-source":
			return "source"
		case "outputs":
			if p[len(p)-1] != "review.md" && p[len(p)-1] != "submission.zip" {
				return "artifact"
			}
		}
	}
	return ""
}
func (a *App) workspaceFiles(c workspaceCourse) []M {
	rows := []M{}
	roots := []string{filepath.Join(c.Dir, "courseware"), filepath.Join(c.Dir, "notices")}
	for _, kind := range []string{"source", "outputs", "report-source"} {
		paths, _ := filepath.Glob(filepath.Join(c.Dir, "homework", "*", kind))
		roots = append(roots, paths...)
	}
	for _, root := range roots {
		for _, file := range visibleFiles(root) {
			rel := filepath.ToSlash(relative(c.Dir, file))
			kind := publicCourseFile(rel)
			if kind == "" {
				continue
			}
			info, e := os.Stat(file)
			if e != nil {
				continue
			}
			title := filepath.Base(file)
			if kind == "notice" {
				var meta M
				if data, e := os.ReadFile(strings.TrimSuffix(file, ".md") + ".json"); e == nil && json.Unmarshal(data, &meta) == nil {
					title = strDefault(meta, "title", title)
				}
			}
			rows = append(rows, M{"title": title, "course_id": c.ID, "course": c.Name, "semester": c.Semester, "path": rel, "name": filepath.Base(file), "kind": kind, "size": info.Size(), "modified_at": info.ModTime().UTC().Format(time.RFC3339), "editable": strings.HasPrefix(rel, "courseware/user/")})
		}
	}
	sort.Slice(rows, func(i, j int) bool { return str(rows[i], "modified_at") > str(rows[j], "modified_at") })
	return rows
}
func (a *App) workspaceHealth() M {
	h := readMap(a.data("daemon-health.json"))
	keep := readMap(filepath.Join(filepath.Dir(a.session()), "keepalive-status.json"))
	cfg := a.config()
	now := time.Now()
	if str(keep, "state") == "valid" && !workspaceFresh(str(keep, "last_success"), now, 15*time.Minute) {
		keep["state"] = "stale"
		keep["message"] = "保活记录待更新"
	}
	writer, reviewer := configuredHarnesses(cfg)
	cli := []M{}
	var checks []M
	if e := attempt(func() {
		if exists(a.data("cli-health.json")) {
			check(json.Unmarshal(readBytes(a.data("cli-health.json")), &checks))
		}
	}); e != nil {
		checks = nil
	}
	for _, tool := range []string{"claude", "codex"} {
		entry := M{"tool": tool, "required": tool == writer || tool == reviewer, "state": "unknown", "reason": "尚未检查"}
		for _, check := range checks {
			if str(check, "tool") == tool {
				entry["state"] = "unavailable"
				if boolean(check, "usable") {
					entry["state"] = "ready"
				}
				entry["reason"] = strings.TrimSpace(str(check, "reason") + " " + str(check, "action"))
				if !workspaceFresh(str(check, "checked_at"), now, 10*time.Minute) {
					entry["state"] = "stale"
					entry["reason"] = "检查记录待更新"
				}
			}
		}
		execution := readMap(a.data(tool + "-execution.json"))
		if str(execution, "state") == "failed" || str(execution, "state") == "running" {
			entry["state"] = execution["state"]
			entry["reason"] = strings.TrimSpace(strDefault(execution, "reason", str(execution, "phase")) + " " + str(execution, "action"))
		}
		cli = append(cli, entry)
	}
	return M{"daemon": M{"state": h["state"], "phase": h["phase"], "time": h["time"], "problem": daemonProblem(h, time.Now())}, "learn": M{"state": keep["state"], "message": keep["message"], "last_success": keep["last_success"]}, "lark": M{"enabled": a.enabled(), "actions": readMap(a.data("actions-health.json"))["state"], "messages": readMap(a.data("messages-health.json"))["state"]}, "cli": cli, "writer": writer, "reviewer": reviewer, "poll_hours": float64(number(cfg, "poll_interval_seconds", 43200)) / 3600, "next_scan": a.nextScan(readMap(a.data("schedule.json"))).Format(time.RFC3339), "scan": readMap(a.data("workspace-scan.json")), "version": Version}
}
func (a *App) workspaceOverview() M {
	courses, tasks := []M{}, []M{}
	allTasks := a.tasks()
	for _, c := range a.workspaceCourses() {
		count := 0
		for _, st := range allTasks {
			if courseContainsTask(c, st) {
				count++
				item := editorTaskSummary(st)
				merge(item, M{"course_id": c.ID, "semester": c.Semester, "deadline": st["deadline"], "updated_at": st["updated_at"], "artifact_count": len(objects(st["artifacts"]))})
				tasks = append(tasks, item)
			}
		}
		courses = append(courses, M{"id": c.ID, "name": c.Name, "semester": c.Semester, "tasks": count})
	}
	return M{"courses": courses, "tasks": tasks, "health": a.workspaceHealth()}
}
func (a *App) workspaceTask(id string) M {
	st := readMap(a.taskPath(id))
	ensure(len(st) > 0, "找不到这份作业")
	result := editorTaskSummary(st)
	merge(result, M{"description": st["description"], "deadline": st["deadline"], "summary": st["summary"], "updated_at": st["updated_at"], "reviews": []M{}, "revisions": []M{}})
	reviews := []M{}
	for _, r := range objects(st["review_history"]) {
		row := M{}
		for _, key := range []string{"id", "revision", "round", "approved", "summary", "comments", "checks", "limitations", "reviewed_at", "reviewer", "writer"} {
			row[key] = r[key]
		}
		reviews = append(reviews, row)
	}
	result["reviews"] = reviews
	for _, c := range a.workspaceCourses() {
		if !courseContainsTask(c, st) {
			continue
		}
		result["course_id"] = c.ID
		attachments := []M{}
		if source, e := filepath.EvalSymlinks(str(st, "folder")); e == nil {
			for _, file := range visibleFiles(source) {
				rel, e := filepath.Rel(c.Dir, file)
				if e != nil {
					continue
				}
				rel = filepath.ToSlash(rel)
				if publicCourseFile(rel) != "assignment" {
					continue
				}
				info, e := os.Stat(file)
				if e == nil {
					attachments = append(attachments, M{"course_id": c.ID, "path": rel, "name": filepath.Base(file), "size": info.Size(), "kind": "assignment"})
				}
			}
		}
		result["attachments"] = attachments
		histories := append([]M{}, objects(st["revision_history"])...)
		histories = append(histories, st)
		revisions := []M{}
		seen := map[int]bool{}
		for i := len(histories) - 1; i >= 0; i-- {
			r := histories[i]
			n := number(r, "revision", 1)
			if seen[n] {
				continue
			}
			seen[n] = true
			files := []M{}
			for _, artifact := range objects(r["artifacts"]) {
				p := str(artifact, "path")
				canonical, err := filepath.EvalSymlinks(p)
				if err != nil {
					continue
				}
				rel, err := filepath.Rel(c.Dir, canonical)
				if err != nil {
					continue
				}
				rel = filepath.ToSlash(rel)
				if publicCourseFile(rel) != "artifact" || !exists(p) {
					continue
				}
				info, e := os.Stat(p)
				if e != nil {
					continue
				}
				files = append(files, M{"course_id": c.ID, "path": rel, "name": filepath.Base(p), "size": info.Size(), "kind": "artifact", "revision": n})
			}
			if len(files) > 0 {
				revisions = append(revisions, M{"revision": n, "files": files})
			}
		}
		result["revisions"] = revisions
		break
	}
	return result
}
func (a *App) workspaceFile(c workspaceCourse, rel string) string {
	ensure(publicCourseFile(rel) != "", "此文件不在课程资料或交付产物中")
	p := c.Dir
	for _, part := range strings.Split(rel, "/") {
		p = filepath.Join(p, part)
		info, e := os.Lstat(p)
		check(e)
		ensure(info.Mode()&os.ModeSymlink == 0, "不允许符号链接文件")
	}
	info, e := os.Stat(p)
	check(e)
	ensure(info.Mode().IsRegular(), "只能访问普通文件")
	return p
}
func (a *App) workspaceFilePreview(c workspaceCourse, rel string) M {
	p := a.workspaceFile(c, rel)
	info, e := os.Stat(p)
	check(e)
	ext := strings.ToLower(filepath.Ext(p))
	result := M{"name": filepath.Base(p), "size": info.Size(), "kind": "download"}
	switch ext {
	case ".pdf":
		result["kind"] = "pdf"
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		result["kind"] = "image"
	case ".html", ".htm":
		result["kind"] = "html"
	case ".zip":
		z, e := zip.OpenReader(p)
		check(e)
		defer z.Close()
		entries := []M{}
		for _, f := range z.File {
			if !f.FileInfo().IsDir() {
				entries = append(entries, M{"name": f.Name, "size": f.UncompressedSize64})
			}
			if len(entries) >= 500 {
				break
			}
		}
		merge(result, M{"kind": "zip", "entries": entries, "truncated": len(z.File) > 500})
	default:
		if info.Size() <= 1<<20 {
			b := readBytes(p)
			if utf8.Valid(b) && !strings.ContainsRune(string(b), 0) {
				merge(result, M{"kind": "text", "text": string(b)})
			}
		}
	}
	return result
}
func (a *App) workspaceUpload(w http.ResponseWriter, r *http.Request, c workspaceCourse) M {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<20)
	check(r.ParseMultipartForm(1 << 20))
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	f, header, e := r.FormFile("file")
	check(e)
	defer f.Close()
	name := safeName(filepath.Base(header.Filename))
	ensure(name != "" && name != "index.json" && !strings.HasPrefix(name, "."), "文件名无效")
	defer a.lock("workspace-"+c.ID, true)()
	dir := filepath.Join(c.Dir, "courseware", "user")
	workspaceUploadDir(c)
	dest := filepath.Join(dir, name)
	ensure(workspaceAbsent(dest), "已有同名文件，请先重命名")
	temp, e := os.CreateTemp(dir, ".upload-*")
	check(e)
	defer os.Remove(temp.Name())
	defer temp.Close()
	_, e = io.Copy(temp, f)
	check(e)
	check(temp.Close())
	check(replaceFile(temp.Name(), dest))
	return M{"name": name, "message": "已加入补充资料。下次扫描会检测材料更新。"}
}
func (a *App) workspaceFileAction(c workspaceCourse, in M) M {
	defer a.lock("workspace-"+c.ID, true)()
	rel := str(in, "path")
	ensure(strings.HasPrefix(rel, "courseware/user/"), "只能重命名或移除自己上传的补充资料")
	p := a.workspaceFile(c, rel)
	switch str(in, "action") {
	case "rename":
		name := strings.TrimSpace(str(in, "name"))
		ensure(name != "" && name == safeName(name) && filepath.Base(name) == name && !strings.ContainsAny(name, "/\\") && !strings.HasPrefix(name, "."), "文件名无效")
		dest := filepath.Join(filepath.Dir(p), name)
		ensure(workspaceAbsent(dest), "已有同名文件")
		check(os.Rename(p, dest))
		return M{"message": "已重命名"}
	case "remove":
		dir := a.data("workspace-trash", randomID(12))
		mkdir(dir)
		writeJSON(filepath.Join(dir, "receipt.json"), M{"course_id": c.ID, "path": rel, "removed_at": stamp()})
		check(os.Rename(p, filepath.Join(dir, filepath.Base(p))))
		return M{"message": "已移入本机回收目录"}
	}
	panic(fmt.Errorf("无效文件操作"))
}
func (a *App) workspaceAPI(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/api/workspace" && !strings.HasPrefix(r.URL.Path, "/api/workspace/") {
		return false
	}
	var result any
	e := attempt(func() {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		action := ""
		if len(parts) > 2 {
			action = parts[2]
		}
		switch {
		case r.Method == "GET" && action == "":
			result = a.workspaceOverview()
		case r.Method == "GET" && action == "health":
			result = a.workspaceHealth()
		case r.Method == "GET" && action == "files":
			rows := []M{}
			for _, c := range a.workspaceCourses() {
				if id := r.URL.Query().Get("course"); id == "" || id == c.ID {
					rows = append(rows, a.workspaceFiles(c)...)
				}
			}
			result = rows
		case r.Method == "GET" && action == "task" && len(parts) == 4:
			result = a.workspaceTask(parts[3])
		case (action == "preview" || action == "file") && r.Method == "GET":
			c := a.workspaceCourse(r.URL.Query().Get("course"))
			rel := r.URL.Query().Get("path")
			if action == "preview" {
				result = a.workspaceFilePreview(c, rel)
				break
			}
			p := a.workspaceFile(c, rel)
			disposition := "attachment"
			if r.URL.Query().Get("inline") == "1" {
				disposition = "inline"
			}
			w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": filepath.Base(p)}))
			policy := "default-src 'none'; img-src 'self' data:; style-src 'unsafe-inline'; sandbox; frame-ancestors 'self'"
			if strings.EqualFold(filepath.Ext(p), ".pdf") {
				policy = "frame-ancestors 'self'; base-uri 'none'"
			}
			w.Header().Set("Content-Security-Policy", policy)
			http.ServeFile(w, r, p)
		case action == "upload" && r.Method == "POST":
			result = a.workspaceUpload(w, r, a.workspaceCourse(r.URL.Query().Get("course")))
		case action == "file-action" && r.Method == "POST":
			in := M{}
			check(decodeWorkspaceJSON(w, r, &in))
			result = a.workspaceFileAction(a.workspaceCourse(str(in, "course")), in)
		case action == "settings" && r.Method == "POST":
			in := M{}
			check(decodeWorkspaceJSON(w, r, &in))
			writer, reviewer := str(in, "writer"), str(in, "reviewer")
			ensure(validHarness(writer) && validHarness(reviewer), "请选择 Claude 或 Codex")
			hours, ok := in["poll_hours"].(float64)
			ensure(ok && hours >= 1.0/60 && hours <= 8760, "课程扫描间隔应为 1 分钟至 365 天")
			a.configure([]string{"--writer", writer, "--reviewer", reviewer, "--poll-interval", strconv.FormatFloat(hours, 'f', -1, 64) + "h"})
			a.wakeEditor()
			result = a.workspaceHealth()
		case action == "scan" && r.Method == "POST":
			defer a.lock("workspace-scan", true)()
			m := readMap(a.data("workspace-scan.json"))
			if state := str(m, "state"); state != "queued" && state != "running" {
				m = M{"id": randomID(12), "state": "queued", "requested_at": stamp()}
				writeJSON(a.data("workspace-scan.json"), m)
			}
			a.wakeEditor()
			result = m
		default:
			panic(fmt.Errorf("无效工作台请求"))
		}
	})
	if e != nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(409)
		writeWorkspaceJSON(w, M{"error": safeError(e)})
	} else if result != nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		writeWorkspaceJSON(w, result)
	}
	return true
}
func decodeWorkspaceJSON(w http.ResponseWriter, r *http.Request, in *M) error {
	return json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(in)
}
func writeWorkspaceJSON(w http.ResponseWriter, value any) { _ = json.NewEncoder(w).Encode(value) }

func workspaceAbsent(path string) bool { _, err := os.Lstat(path); return os.IsNotExist(err) }
func workspaceUploadDir(c workspaceCourse) {
	p := c.Dir
	for _, part := range []string{"courseware", "user"} {
		p = filepath.Join(p, part)
		if e := os.Mkdir(p, 0700); e != nil && !os.IsExist(e) {
			check(e)
		}
		info, e := os.Lstat(p)
		check(e)
		ensure(info.IsDir() && info.Mode()&os.ModeSymlink == 0, "补充资料目录不能使用符号链接")
	}
}

func workspaceFresh(value string, now time.Time, maxAge time.Duration) bool {
	at := parseTime(value)
	return !at.IsZero() && now.Sub(at) < maxAge && now.Sub(at) >= -time.Minute
}
