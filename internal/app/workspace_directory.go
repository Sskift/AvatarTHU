package app

import (
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// Legacy file URLs remain unchanged. Only their location in the browser is
// mapped to the assignment layout; browsing never moves or publishes files.
func directoryFilePath(rel string) string {
	parts := strings.Split(rel, "/")
	if len(parts) >= 4 && parts[0] == "homework" {
		area := ""
		switch parts[2] {
		case "source":
			area = "workspace"
		case "outputs", "report-source":
			area = "presentation"
		}
		if area != "" {
			parts = append(append(append([]string{}, parts[:2]...), area), parts[2:]...)
		}
	}
	return strings.Join(parts, "/")
}

func (a *App) workspaceDirectory(c workspaceCourse, rel string) M {
	ensure(rel == "" || (!filepath.IsAbs(rel) && !strings.ContainsAny(rel, "\\:") && path.Clean(rel) == rel), "无效目录")
	for _, part := range strings.Split(rel, "/") {
		ensure(!strings.HasPrefix(part, "."), "无效目录")
	}
	dirs, files := map[string]M{}, map[string]M{}
	var addDir func(string)
	addDir = func(p string) {
		if p == "" || p == "." {
			return
		}
		if _, ok := dirs[p]; !ok {
			dirs[p] = M{"name": path.Base(p), "title": path.Base(p), "path": p, "kind": "directory"}
			addDir(path.Dir(p))
		}
	}
	addFile := func(file M, display string) {
		// Match download authorization, including symlink restrictions.
		if attempt(func() { a.workspaceFile(c, str(file, "path")) }) != nil {
			return
		}
		file["browse_path"] = display
		files[display] = file
		addDir(path.Dir(display))
	}
	for _, p := range []string{"courseware", "notices", "homework"} {
		addDir(p)
	}
	for _, file := range a.workspaceFiles(c) {
		addFile(file, directoryFilePath(str(file, "path")))
	}
	// School filenames may be opaque IDs; keep their readable courseware title.
	var index []M
	if data, err := os.ReadFile(filepath.Join(c.Dir, "courseware", "index.json")); err == nil && json.Unmarshal(data, &index) == nil {
		for _, item := range index {
			p := "courseware/" + str(item, "path")
			if file := files[p]; file != nil && str(item, "title") != "" {
				file["title"] = item["title"]
				if parent := path.Dir(p); parent != "courseware" && dirs[parent] != nil {
					dirs[parent]["title"] = item["title"]
				}
			}
		}
	}
	for _, st := range a.tasks() {
		if !courseContainsTask(c, st) || str(st, "assignment_dir") == "" {
			continue
		}
		base, err := filepath.EvalSymlinks(assignmentRoot(str(st, "assignment_dir")))
		if err != nil {
			continue
		}
		assignment := filepath.ToSlash(relative(c.Dir, base))
		parts := strings.Split(assignment, "/")
		if len(parts) != 2 || parts[0] != "homework" || strings.HasPrefix(parts[1], ".") {
			continue
		}
		addDir(assignment)
		dirs[assignment]["title"] = strDefault(st, "title", parts[1])
		for _, area := range []string{"workspace", "presentation"} {
			addDir(assignment + "/" + area)
		}
		folder, archive := filepath.Join(base, "submission"), filepath.Join(base, "submission.zip")
		if !usesAssignmentLayout(str(st, "assignment_dir")) {
			folder, archive = filepath.Join(a.outputDir(st), "submission"), filepath.Join(a.outputDir(st), "submission.zip")
		}
		if info, err := os.Stat(folder); err == nil && info.IsDir() {
			addDir(assignment + "/submission")
			for _, file := range visibleFiles(folder) {
				info, err := os.Stat(file)
				if err != nil {
					continue
				}
				actual := filepath.ToSlash(relative(c.Dir, file))
				addFile(M{"course_id": c.ID, "path": actual, "name": filepath.Base(file), "kind": "submission", "size": info.Size(), "modified_at": info.ModTime().UTC().Format("2006-01-02T15:04:05Z")}, assignment+"/submission/"+filepath.ToSlash(relative(folder, file)))
			}
		}
		if info, err := os.Stat(archive); err == nil && info.Mode().IsRegular() {
			actual := filepath.ToSlash(relative(c.Dir, archive))
			addFile(M{"course_id": c.ID, "path": actual, "name": "submission.zip", "kind": "submission", "size": info.Size(), "modified_at": info.ModTime().UTC().Format("2006-01-02T15:04:05Z")}, assignment+"/submission.zip")
		}
	}
	labels := map[string]string{"courseware": "课件与补充资料", "notices": "课程公告", "homework": "作业", "submission": "当前提交内容", "workspace": "原题与工作资料", "presentation": "展示、源文件与历史版本"}
	for p, dir := range dirs {
		if label := labels[path.Base(p)]; label != "" {
			dir["description"] = label
		}
	}
	ensure(rel == "" || dirs[rel] != nil, "找不到这个目录")
	entries := []M{}
	parent := rel
	if parent == "" {
		parent = "."
	}
	for p, dir := range dirs {
		if path.Dir(p) == parent {
			entries = append(entries, dir)
		}
	}
	for p, file := range files {
		if path.Dir(p) == parent {
			entries = append(entries, file)
		}
	}
	order := map[string]int{"submission": 1, "submission.zip": 2, "workspace": 3, "presentation": 4}
	sort.Slice(entries, func(i, j int) bool {
		left, right := entries[i], entries[j]
		if len(strings.Split(rel, "/")) == 2 && strings.HasPrefix(rel, "homework/") {
			if order[str(left, "name")] != order[str(right, "name")] {
				return order[str(left, "name")] < order[str(right, "name")]
			}
		}
		if (str(left, "kind") == "directory") != (str(right, "kind") == "directory") {
			return str(left, "kind") == "directory"
		}
		return str(left, "title")+str(left, "name") < str(right, "title")+str(right, "name")
	})
	crumbs := []M{{"path": "", "title": c.Name}}
	if rel != "" {
		parts := strings.Split(rel, "/")
		for i := range parts {
			p := strings.Join(parts[:i+1], "/")
			crumbs = append(crumbs, M{"path": p, "title": dirs[p]["title"]})
		}
	}
	return M{"path": rel, "entries": entries, "breadcrumbs": crumbs}
}
