package app

import (
	"archive/zip"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

// Materialize the frozen upload, not the independently downloadable archives.
// Never link to the original: editing a local copy must not change a card's files.
func materializeSubmission(source, destination string) {
	info, err := os.Lstat(source)
	check(err)
	ensure(info.Mode().IsRegular(), "提交文件必须是普通文件")
	mkdir(filepath.Dir(destination))
	parent, err := os.Lstat(filepath.Dir(destination))
	check(err)
	ensure(parent.IsDir() && parent.Mode()&os.ModeSymlink == 0, "最终提交目录的父目录不能是链接或特殊文件")
	temporary, err := os.MkdirTemp(filepath.Dir(destination), ".submission-directory-")
	check(err)
	defer os.RemoveAll(temporary)
	type member struct {
		name string
		hash string
		mode os.FileMode
	}
	files := map[string]member{}
	entries, directories := map[string]bool{}, map[string]string{}
	directory := func(name string) {
		key := strings.ToLower(name)
		ensure(directories[key] == "" || directories[key] == name, "提交包目录大小写冲突："+name)
		directories[key] = name
	}
	var total int64
	add := func(name string, mode os.FileMode, reader io.Reader) {
		name = submissionName(name)
		ensure(filepath.IsLocal(filepath.FromSlash(name)), "提交包包含无效路径："+name)
		key := strings.ToLower(name)
		ensure(!entries[key], "提交包包含重复路径："+name)
		entries[key] = true
		ensure(len(entries) <= 50000, "提交包文件数量过多")
		ensure(mode.IsRegular() || mode.IsDir(), "提交包不能包含链接或特殊文件："+name)
		for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
			_, file := files[strings.ToLower(parent)]
			ensure(!file, "提交包文件与目录重名："+name)
			directory(parent)
		}
		target := filepath.Join(temporary, filepath.FromSlash(name))
		if mode.IsDir() {
			_, file := files[key]
			ensure(!file, "提交包文件与目录重名："+name)
			directory(name)
			mkdir(target)
			return
		}
		ensure(directories[key] == "", "提交包文件与目录重名："+name)
		mkdir(filepath.Dir(target))
		f, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		check(err)
		h := sha256.New()
		n, copyErr := io.Copy(io.MultiWriter(f, h), io.LimitReader(reader, (2<<30)-total+1))
		closeErr := f.Close()
		check(copyErr)
		check(closeErr)
		total += n
		ensure(total <= 2<<30, "提交包展开后超过 2 GB")
		permission := mode.Perm()
		if permission == 0 {
			permission = 0600
		}
		check(os.Chmod(target, permission))
		files[key] = member{name, fmt.Sprintf("%x", h.Sum(nil)), permission}
	}
	if strings.EqualFold(filepath.Ext(source), ".zip") {
		archive, err := zip.OpenReader(source)
		check(err)
		defer archive.Close()
		for _, entry := range archive.File {
			func() {
				stream, err := entry.Open()
				check(err)
				defer stream.Close()
				add(entry.Name, entry.Mode(), stream)
			}()
		}
	} else {
		f, err := os.Open(source)
		check(err)
		defer f.Close()
		add(filepath.Base(source), info.Mode(), f)
	}
	ensure(len(files) > 0, "提交内容为空")
	if existing, err := os.Lstat(destination); err == nil {
		ensure(existing.IsDir() && existing.Mode()&os.ModeSymlink == 0, "最终提交目录不是普通目录："+destination)
		seen := map[string]bool{}
		seenDirectories := map[string]bool{}
		check(filepath.WalkDir(destination, func(p string, entry os.DirEntry, err error) error {
			check(err)
			if p == destination {
				return nil
			}
			name := filepath.ToSlash(relative(destination, p))
			key := strings.ToLower(name)
			st, err := entry.Info()
			check(err)
			ensure(st.Mode().IsRegular() || st.IsDir(), "最终提交目录包含链接或特殊文件："+name)
			if st.IsDir() {
				ensure(directories[key] == name, "最终提交目录已有额外目录或路径大小写改变，请保留修改后移出该目录："+name)
				seenDirectories[key] = true
				return nil
			}
			want, ok := files[key]
			ensure(ok && name == want.name && !seen[key] && digest(p) == want.hash, "最终提交目录已被修改或含额外文件，请保留修改后移出该目录："+name)
			if runtime.GOOS != "windows" {
				ensure(st.Mode().Perm() == want.mode, "最终提交目录的文件权限已被修改："+name)
			}
			seen[key] = true
			return nil
		}))
		ensure(len(seen) == len(files), "最终提交目录缺少文件，请保留修改后移出该目录："+destination)
		ensure(len(seenDirectories) == len(directories), "最终提交目录缺少子目录，请保留修改后移出该目录："+destination)
		return
	} else {
		ensure(os.IsNotExist(err), "无法检查最终提交目录："+destination)
	}
	check(os.Rename(temporary, destination))
}

func (a *App) submissionFolder(tid string) string {
	defer a.lock(tid, true)()
	st := readMap(a.taskPath(tid))
	ensure(len(st) > 0 && str(st, "submission") != "", "这份作业还没有冻结的提交文件")
	return a.publishSubmission(st)
}
