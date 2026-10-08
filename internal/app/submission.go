package app

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type submissionPacker struct {
	zip   *zip.Writer
	files map[string]string
	size  int64
	count int
}

func submissionName(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	clean := path.Clean(name)
	ensure(clean != "." && clean != ".." && !strings.HasPrefix(clean, "../") && !strings.HasPrefix(name, "/") && !strings.Contains(name, ":"), "提交包包含无效路径："+name)
	return clean
}

func (p *submissionPacker) add(name string, info os.FileInfo, r io.Reader, depth int) {
	name = submissionName(name)
	p.count++
	ensure(p.count <= 50000 && depth <= 8, "提交包层级或文件数量过多")
	ensure(info.Mode().IsRegular() || info.IsDir(), "提交包不能包含链接或特殊文件："+name)
	if info.IsDir() {
		return
	}
	runtimeData := strings.HasSuffix(strings.ToLower(name), "/_internal/base_library.zip")
	if strings.EqualFold(path.Ext(name), ".zip") && !runtimeData {
		data, err := io.ReadAll(io.LimitReader(r, (256<<20)+1))
		check(err)
		ensure(len(data) <= 256<<20, "待展开的 ZIP 超过 256 MB："+name)
		archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		check(err)
		for _, entry := range archive.File {
			func() {
				member := submissionName(entry.Name)
				stream, err := entry.Open()
				check(err)
				defer stream.Close()
				p.add(path.Join(path.Dir(name), member), entry.FileInfo(), stream, depth+1)
			}()
		}
		return
	}
	key := strings.ToLower(name)
	h := sha256.New()
	var out io.Writer = h
	if _, exists := p.files[key]; !exists {
		for parent := path.Dir(key); parent != "."; parent = path.Dir(parent) {
			_, exists := p.files[parent]
			ensure(!exists, "提交包文件与目录重名："+name)
		}
		for existing := range p.files {
			ensure(!strings.HasPrefix(existing, key+"/"), "提交包文件与目录重名："+name)
		}
		header, err := zip.FileInfoHeader(info)
		check(err)
		header.Name, header.Method = name, zip.Deflate
		entry, err := p.zip.CreateHeader(header)
		check(err)
		out = io.MultiWriter(entry, h)
	}
	n, err := io.Copy(out, io.LimitReader(r, (2<<30)-p.size+1))
	check(err)
	p.size += n
	ensure(p.size <= 2<<30, "提交包展开后超过 2 GB")
	hash := fmt.Sprintf("%x", h.Sum(nil))
	if previous, exists := p.files[key]; exists {
		ensure(previous == hash, "提交包中有内容不同的同名文件，请主写整理目录："+name)
	}
	p.files[key] = hash
}

func buildSubmissionZIP(destination string, files []string) {
	mkdir(filepath.Dir(destination))
	f, err := os.CreateTemp(filepath.Dir(destination), ".submission-*.zip")
	check(err)
	defer os.Remove(f.Name())
	defer f.Close()
	z := zip.NewWriter(f)
	p := submissionPacker{zip: z, files: map[string]string{}}
	for _, file := range files {
		func() {
			info, err := os.Lstat(file)
			check(err)
			ensure(info.Mode().IsRegular(), "提交产物必须是普通文件")
			src, err := os.Open(file)
			check(err)
			defer src.Close()
			p.add(filepath.Base(file), info, src, 0)
		}()
	}
	check(z.Close())
	check(f.Sync())
	check(f.Close())
	check(replaceFile(f.Name(), destination))
}

func (a *App) exportSubmission(tid, selected string) string {
	defer a.lock(tid, true)()
	st := readMap(a.taskPath(tid))
	ensure(len(st) > 0 && len(objects(st["artifacts"])) > 0, "这份作业还没有可导出的产物")
	a.verifiedFiles(st)
	if selected == "" && str(st, "assignment_dir") != "" && usesAssignmentLayout(str(st, "assignment_dir")) {
		folder := a.publishSubmission(st)
		return filepath.Join(filepath.Dir(folder), "submission.zip")
	}
	files := []string{}
	for _, artifact := range objects(st["artifacts"]) {
		file := str(artifact, "path")
		if selected == "" || filepath.Base(file) == selected {
			files = append(files, file)
		}
	}
	ensure(len(files) > 0, "找不到指定产物："+selected)
	base := strDefault(st, "assignment_dir", a.data("jobs", tid))
	name := "submission.zip"
	if selected != "" && strings.EqualFold(filepath.Ext(selected), ".zip") {
		name = selected
	}
	destination := assignmentPath(base, "exports", fmt.Sprintf("r%d", number(st, "revision", 1)), name)
	buildSubmissionZIP(destination, files)
	return destination
}
