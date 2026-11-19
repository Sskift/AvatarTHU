package app

import (
	"archive/zip"
	"bytes"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const maxPreviewMember = 32 << 20

func previewKind(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".pdf":
		return "pdf"
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		return "image"
	case ".html", ".htm":
		return "html"
	case ".zip":
		return "zip"
	}
	return "download"
}

func filePreview(name string, size int64, read func() []byte) M {
	kind := previewKind(name)
	result := M{"name": name, "size": size, "kind": kind}
	if kind == "download" && size <= 1<<20 {
		b := read()
		if utf8.Valid(b) && bytes.IndexByte(b, 0) == -1 {
			merge(result, M{"kind": "text", "text": string(b)})
		}
	}
	return result
}

func workspaceFileHeaders(w http.ResponseWriter, r *http.Request, name string) {
	disposition := "attachment"
	if r.URL.Query().Get("inline") == "1" {
		disposition = "inline"
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": name}))
	policy := "default-src 'none'; img-src 'self' data:; style-src 'unsafe-inline'; sandbox; frame-ancestors 'self'"
	if previewKind(name) == "pdf" {
		policy = "frame-ancestors 'self'; base-uri 'none'"
	}
	w.Header().Set("Content-Security-Policy", policy)
}

func browsableMember(f *zip.File) bool {
	return f.Mode().IsRegular() && fs.ValidPath(f.Name) && !strings.ContainsAny(f.Name, "\\:") &&
		f.UncompressedSize64 <= maxPreviewMember && previewKind(f.Name) != "zip"
}

func (a *App) workspaceReadFile(w http.ResponseWriter, r *http.Request, c workspaceCourse, preview bool) M {
	query := r.URL.Query()
	rel, member := query.Get("path"), query.Get("member")
	p := a.workspaceFile(c, rel)
	if member == "" && !preview {
		workspaceFileHeaders(w, r, filepath.Base(p))
		http.ServeFile(w, r, p)
		return nil
	}
	if previewKind(p) != "zip" {
		ensure(member == "", "此文件不是 ZIP")
		info, err := os.Stat(p)
		check(err)
		return filePreview(filepath.Base(p), info.Size(), func() []byte { return readBytes(p) })
	}

	archive, err := zip.OpenReader(p)
	check(err)
	defer archive.Close()
	// A named preview must identify a unique entry in the same archive.
	count := make(map[string]int)
	for _, f := range archive.File {
		count[f.Name]++
	}
	if member != "" {
		ensure(count[member] == 1, "包内文件不存在或名称重复")
		for _, f := range archive.File {
			if f.Name != member {
				continue
			}
			ensure(browsableMember(f), "此包内文件请下载完整 ZIP 后查看")
			read := func() []byte {
				reader, err := f.Open()
				check(err)
				defer reader.Close()
				b, err := io.ReadAll(io.LimitReader(reader, maxPreviewMember+1))
				check(err)
				ensure(uint64(len(b)) == f.UncompressedSize64, "包内文件大小与索引不一致")
				return b
			}
			name := path.Base(member)
			if preview {
				return filePreview(name, int64(f.UncompressedSize64), read)
			}
			b := read()
			workspaceFileHeaders(w, r, name)
			http.ServeContent(w, r, name, f.Modified, bytes.NewReader(b))
			return nil
		}
	}

	entries := []M{}
	truncated := false
	for _, f := range archive.File {
		if f.FileInfo().IsDir() {
			continue
		}
		if len(entries) == 500 {
			truncated = true
			break
		}
		entry := M{"name": f.Name, "size": f.UncompressedSize64}
		if count[f.Name] == 1 && browsableMember(f) {
			entry["file"] = M{"name": path.Base(f.Name), "size": f.UncompressedSize64, "course_id": c.ID, "path": rel, "member": f.Name}
		}
		entries = append(entries, entry)
	}
	info, err := os.Stat(p)
	check(err)
	return M{"name": filepath.Base(p), "size": info.Size(), "kind": "zip", "entries": entries, "truncated": truncated}
}
