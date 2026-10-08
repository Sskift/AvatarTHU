package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// A migrated legacy root stays available to old receipts and recorded commands.
func assignmentRoot(base string) string {
	if root := str(readMap(filepath.Join(base, ".layout.json")), "root"); root != "" {
		ensure(filepath.IsAbs(root), "作业目录映射必须是绝对路径")
		return filepath.Clean(root)
	}
	return base
}

func usesAssignmentLayout(base string) bool {
	if assignmentRoot(base) != base || exists(filepath.Join(base, "workspace", "assignment.json")) {
		return true
	}
	info, err := os.Stat(filepath.Join(base, "source"))
	return err != nil || !info.IsDir()
}

func assignmentPath(base, kind string, parts ...string) string {
	root := assignmentRoot(base)
	area := "workspace"
	if kind == "outputs" || kind == "reviews" || kind == "report-source" {
		area = "presentation"
	}
	// Existing installations remain readable until explicitly reorganized.
	if !usesAssignmentLayout(base) {
		return filepath.Join(append([]string{base, kind}, parts...)...)
	}
	return filepath.Join(append([]string{root, area, kind}, parts...)...)
}

func englishName(name string) string {
	name = safeName(name)
	for _, r := range name {
		if r > 127 {
			ext := filepath.Ext(name)
			for _, r := range ext {
				if r > 127 {
					ext = ""
					break
				}
			}
			return "file-" + fingerprint(name)[:12] + ext
		}
	}
	return strings.ReplaceAll(name, " ", "-")
}

func (a *App) publishSubmission(st M) string {
	base := str(st, "assignment_dir")
	if base == "" || !usesAssignmentLayout(base) {
		dest := filepath.Join(a.outputDir(st), "submission")
		materializeSubmission(a.verifiedFiles(st), dest)
		return dest
	}
	root := assignmentRoot(base)
	folder, archive := filepath.Join(root, "submission"), filepath.Join(root, "submission.zip")
	manifest := assignmentPath(base, "submission.json")
	previous := readMap(manifest)
	source := a.verifiedFiles(st)
	manual := boolean(previous, "manual") && str(previous, "base_sha256") == str(st, "sha256") && number(previous, "revision", 0) == number(st, "revision", 1)
	if manual {
		source = str(previous, "source")
		ensure(digest(source) == str(previous, "source_sha256"), "手动提交原件已改变，请重新核对")
	}
	mkdir(filepath.Dir(manifest))
	stage, err := os.MkdirTemp(filepath.Dir(manifest), "submission-")
	check(err)
	defer os.RemoveAll(stage)
	bundle := filepath.Join(stage, "submission.zip")
	if strings.EqualFold(filepath.Ext(source), ".zip") {
		copyFile(source, bundle)
	} else {
		buildSubmissionZIP(bundle, []string{source})
	}
	hash := digest(bundle)
	materializeSubmission(bundle, filepath.Join(stage, "submission"))
	if info, err := os.Lstat(archive); err == nil {
		ensure(info.Mode().IsRegular(), "最终提交 ZIP 不能是链接或特殊文件")
		if digest(archive) == hash {
			materializeSubmission(archive, folder)
			if str(previous, "sha256") == hash && str(previous, "base_sha256") == str(st, "sha256") && number(previous, "revision", 0) == number(st, "revision", 1) {
				return folder
			}
		} else {
			ensure(digest(archive) == str(previous, "sha256"), "最终提交 ZIP 已被修改，请先保留修改")
			materializeSubmission(archive, folder)
			backup := assignmentPath(base, "submission-history", fmt.Sprintf("r%d-%s", number(previous, "revision", 0), randomID(4)))
			mkdir(backup)
			check(os.Rename(folder, filepath.Join(backup, "submission")))
			if err := os.Rename(archive, filepath.Join(backup, "submission.zip")); err != nil {
				_ = os.Rename(filepath.Join(backup, "submission"), folder)
				check(err)
			}
		}
	} else {
		ensure(os.IsNotExist(err), "无法读取最终提交 ZIP")
	}
	materializeSubmission(bundle, folder)
	if !exists(archive) {
		copyFile(bundle, archive)
	}
	writeJSON(manifest, M{"revision": st["revision"], "base_sha256": st["sha256"], "sha256": hash, "source": source, "source_sha256": digest(source), "manual": manual})
	return folder
}
