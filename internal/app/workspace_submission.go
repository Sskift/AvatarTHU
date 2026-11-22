package app

import (
	"archive/zip"
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

type submissionReviewFile struct {
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	Available bool   `json:"available"`
	Mode      string `json:"mode,omitempty"`
	hash      string
}

type submissionIndex struct {
	root  M
	files map[string]submissionReviewFile
	dirs  []string
}

func reviewVersion(st M, n int) M {
	if publishedRevision(st) == n && str(st, "submission") != "" {
		return st
	}
	for _, v := range objects(st["revision_history"]) {
		if publishedRevision(v) == n {
			return v
		}
	}
	return nil
}

func (a *App) reviewCourse(st M) workspaceCourse {
	for _, c := range a.workspaceCourses() {
		if courseContainsTask(c, st) {
			return c
		}
	}
	panic(fmt.Errorf("找不到这份作业的课程"))
}

func (a *App) reviewSubmission(c workspaceCourse, version M) (M, string, string) {
	ensure(version != nil && !boolean(version, "artifacts_removed"), "该版提交文件不可用")
	root := workspaceDeliveryFile(c, str(version, "submission"), publishedRevision(version))
	ensure(root != nil, "找不到该版提交文件")
	file := a.workspaceFile(c, str(root, "path"))
	hash := digest(file)
	expected := str(version, "sha256")
	ensure(expected == "" || expected == hash, "提交文件与该版记录不一致")
	root["sha256"] = hash
	return root, file, hash
}

// Inspect the same bytes bound to the version, including explicit empty directories.
// Only unpack file contents for hashing when there are two versions to compare.
func (a *App) submissionIndex(c workspaceCourse, version M, compare bool) submissionIndex {
	root, file, hash := a.reviewSubmission(c, version)
	index := submissionIndex{root: root, files: map[string]submissionReviewFile{}}
	if previewKind(file) != "zip" {
		name := str(root, "name")
		index.files[name] = submissionReviewFile{Name: name, Size: root["size"].(int64), Available: true, hash: hash}
		return index
	}
	archive, err := zip.OpenReader(file)
	check(err)
	defer archive.Close()
	ensure(len(archive.File) <= 50000, "提交包的目录过大")
	var total uint64
	seen := map[string]bool{}
	for _, entry := range archive.File {
		name := strings.TrimSuffix(entry.Name, "/")
		ensure(name != "." && fs.ValidPath(name) && !strings.ContainsAny(name, "\\:") && !seen[name], "提交包目录包含无效或重复路径")
		seen[name] = true
		if entry.FileInfo().IsDir() {
			index.dirs = append(index.dirs, name)
			continue
		}
		ensure(entry.Mode().IsRegular() && entry.UncompressedSize64 <= 2<<30-total, "提交包含特殊文件或展开后超过 2 GB")
		total += entry.UncompressedSize64
		f := submissionReviewFile{Name: name, Size: int64(entry.UncompressedSize64), Available: browsableMember(entry), Mode: fmt.Sprintf("%04o", entry.Mode().Perm())}
		if compare {
			func() {
				stream, err := entry.Open()
				check(err)
				defer stream.Close()
				h := sha256.New()
				n, err := io.Copy(h, io.LimitReader(stream, f.Size+1))
				check(err)
				ensure(n == f.Size, "包内文件大小与记录不一致")
				f.hash = fmt.Sprintf("%x", h.Sum(nil))
			}()
		}
		index.files[name] = f
	}
	return index
}

func (a *App) workspaceSubmissionTree(st M, n int) M {
	c := a.reviewCourse(st)
	previous := reviewVersion(st, n-1)
	var old submissionIndex
	baseline := "first"
	if n > 1 {
		baseline = "unavailable"
		if previous != nil && attempt(func() { old = a.submissionIndex(c, previous, true) }) == nil {
			baseline = "available"
		}
	}
	current := a.submissionIndex(c, reviewVersion(st, n), baseline == "available")
	names := make([]string, 0, len(current.files))
	for name := range current.files {
		names = append(names, name)
	}
	for name := range old.files {
		if _, exists := current.files[name]; !exists {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	files := []M{}
	for _, name := range names {
		row := M{"name": name}
		now, present := current.files[name]
		prev, had := old.files[name]
		if present {
			row["current"] = now
		}
		if had {
			row["previous"] = prev
		}
		switch {
		case baseline != "available":
			row["change"] = baseline
		case !had:
			row["change"] = "added"
		case !present:
			row["change"] = "removed"
		case now.hash != prev.hash || (now.Mode != "" && prev.Mode != "" && now.Mode != prev.Mode):
			row["change"] = "modified"
		default:
			row["change"] = "unchanged"
		}
		files = append(files, row)
	}
	return M{"revision": n, "submission": current.root, "previous": old.root, "baseline": baseline, "directories": current.dirs, "files": files}
}

const reviewTextLimit = 256 << 10

func reviewFileText(file, name string) (string, bool) {
	if previewKind(file) != "zip" {
		ensure(filepath.Base(file) == name, "所选文件不在该版提交件中")
		info, err := os.Stat(file)
		check(err)
		if info.Size() > reviewTextLimit {
			return "", false
		}
		f, err := os.Open(file)
		check(err)
		defer f.Close()
		data := comparisonContent(f)
		return data.Text, data.IsText
	}
	archive, err := zip.OpenReader(file)
	check(err)
	defer archive.Close()
	var match *zip.File
	for _, entry := range archive.File {
		if entry.Name == name {
			ensure(match == nil, "包内文件名重复")
			match = entry
		}
	}
	ensure(match != nil && browsableMember(match), "所选文件不能单独读取")
	if match.UncompressedSize64 > reviewTextLimit {
		return "", false
	}
	reader, err := match.Open()
	check(err)
	defer reader.Close()
	data := comparisonContent(io.LimitReader(reader, int64(match.UncompressedSize64)+1))
	return data.Text, data.IsText
}

func (a *App) workspaceFileChange(st M, n int, name string) M {
	c := a.reviewCourse(st)
	_, previous, _ := a.reviewSubmission(c, reviewVersion(st, n-1))
	_, current, _ := a.reviewSubmission(c, reviewVersion(st, n))
	before, aText := reviewFileText(previous, name)
	after, bText := reviewFileText(current, name)
	if aText && bText && before != after {
		return M{"lines": lineChanges(before, after)}
	}
	return M{}
}

type workspaceTextEdit struct {
	ID       string  `json:"id"`
	Revision int     `json:"revision"`
	Name     string  `json:"name"`
	BaseHash string  `json:"base_hash"`
	Text     *string `json:"text"`
}

func (a *App) acceptWorkspaceText(id string, edit workspaceTextEdit) {
	task := a.taskPath(id)
	defer a.lock(id, false)()
	st := readMap(task)
	ensure(editorRequestID.MatchString(edit.ID) && edit.Text != nil && len(*edit.Text) <= reviewTextLimit &&
		utf8.ValidString(*edit.Text) && !strings.ContainsRune(*edit.Text, 0), "无效文本或文本超过 256 KB")
	sum := fmt.Sprintf("%x", sha256.Sum256([]byte(*edit.Text)))
	last := obj(st, "owner_file_edit")
	if str(last, "id") == edit.ID {
		ensure(str(last, "name") == edit.Name && str(last, "sha256") == sum && number(last, "revision", 0) == edit.Revision+1 &&
			str(last, "base_hash") == edit.BaseHash, "该请求编号已用于另一份修改")
		return
	}
	ensure(editorEditable(st) && edit.Revision == number(st, "revision", 1) && edit.Revision == publishedRevision(st), "作业版本或状态已更新，请核对新版后再修改")
	_, file, hash := a.reviewSubmission(a.reviewCourse(st), st)
	ensure(hash == edit.BaseHash && previewKind(edit.Name) == "download", "文件版本或格式不支持此次编辑")
	original, ok := reviewFileText(file, edit.Name)
	ensure(ok && original != *edit.Text, "请选择有改动的 UTF-8 文本文件")
	writeFile(filepath.Join(a.editorDir(st), "file-edits", edit.ID, "content.txt"), []byte(*edit.Text), 0600)
	st["owner_file_edit"] = M{"id": edit.ID, "revision": edit.Revision + 1, "name": edit.Name, "base_hash": hash, "sha256": sum}
	a.queueRevision(st, fmt.Sprintf("用户已直接编辑提交目录中的 %q。以 owner-file/content.txt 的完整内容更新该文件，保留相对路径和文件权限，并同步受影响的交付文件。", edit.Name))
}
