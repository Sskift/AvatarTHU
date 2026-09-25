package app

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

// Capture only published-version metadata. It is used by the reader's document,
// never included in the independent reviewer's inputs.
func (a *App) rememberRevision(st M) {
	if len(objects(st["artifacts"])) == 0 {
		return
	}
	history := objects(st["revision_history"])
	for _, entry := range history {
		if number(entry, "revision", 0) == number(st, "revision", 1) {
			return
		}
	}
	entry := M{"saved_at": stamp()}
	for _, key := range []string{"revision", "output_dir", "artifacts", "submission", "sha256", "local_review", "review_doc"} {
		entry[key] = st[key]
	}
	// Detach nested maps before the next version replaces delivery metadata.
	entry = parseMap(jsonBytes(entry))
	st["revision_history"] = append(history, entry)
	writeJSON(filepath.Join(a.reviewDir(st), "revision.json"), entry)
	a.saveTask(st)
}

type comparisonFile struct {
	Hash   string
	Text   string
	IsText bool
}

func comparisonContent(r io.Reader) comparisonFile {
	h := sha256.New()
	var text bytes.Buffer
	// Keep only small UTF-8 text in memory; hash larger files as a stream.
	n, e := io.Copy(io.MultiWriter(h, &text), io.LimitReader(r, (256<<10)+1))
	check(e)
	_, e = io.Copy(h, r)
	check(e)
	b := text.Bytes()
	plain := n <= 256<<10 && utf8.Valid(b) && !bytes.ContainsRune(b, 0)
	value := ""
	if plain {
		value = string(b)
	}
	return comparisonFile{hex.EncodeToString(h.Sum(nil)), value, plain}
}

func comparisonFiles(path string) map[string]comparisonFile {
	result := map[string]comparisonFile{}
	f, e := os.Open(path)
	check(e)
	defer f.Close()
	result[filepath.Base(path)] = comparisonContent(f)
	if !strings.EqualFold(filepath.Ext(path), ".zip") {
		return result
	}
	z, e := zip.OpenReader(path)
	check(e)
	defer z.Close()
	ensure(len(z.File) <= 1000, "ZIP 文件过多，未展开内部差异")
	var total uint64
	for _, entry := range z.File {
		if entry.FileInfo().IsDir() {
			continue
		}
		total += entry.UncompressedSize64
		ensure(entry.UncompressedSize64 <= 16<<20 && total <= 64<<20, "ZIP 超出差异预览大小，未展开内部差异")
		name := entry.Name
		ensure(!strings.HasPrefix(name, "/") && !strings.ContainsAny(name, "\\:") && entry.Mode()&os.ModeSymlink == 0, "ZIP 含不支持的路径")
		for _, part := range strings.Split(name, "/") {
			ensure(part != ".." && part != ".", "ZIP 含不支持的路径")
		}
		key := filepath.Base(path) + "/" + name
		ensure(result[key].Hash == "", "ZIP 含重复文件名")
		r, e := entry.Open()
		check(e)
		// The bounded reader also guards a forged uncompressed-size header.
		result[key] = comparisonContent(io.LimitReader(r, (16<<20)+1))
		check(r.Close())
	}
	return result
}

// A bounded LCS produces actual line additions/removals without shelling out to
// diff, so Windows and macOS produce the same document.
func lineChanges(before, after string) string {
	a, b := strings.Split(before, "\n"), strings.Split(after, "\n")
	if len(a) > 1200 || len(b) > 1200 {
		return "文件行数较多，请下载前后版本查看完整差异。"
	}
	width := len(b) + 1
	dp := make([]uint16, (len(a)+1)*width)
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i*width+j] = dp[(i+1)*width+j+1] + 1
			} else {
				dp[i*width+j] = max(dp[(i+1)*width+j], dp[i*width+j+1])
			}
		}
	}
	lines := []string{"- 上版删除的行；+ 本版增加的行；@ 为对应行号"}
	i, j, shown := 0, 0, 0
	for i < len(a) || j < len(b) {
		if i < len(a) && j < len(b) && a[i] == b[j] {
			i++
			j++
			continue
		}
		if shown >= 80 {
			lines = append(lines, "…差异较长，以上为前 80 行；完整内容见前后版本附件。")
			break
		}
		lines = append(lines, fmt.Sprintf("@ 上版 %d 行 / 本版 %d 行", i+1, j+1))
		if i < len(a) && (j == len(b) || dp[(i+1)*width+j] >= dp[i*width+j+1]) {
			lines = append(lines, "- "+cut(a[i], 500))
			i++
		} else {
			lines = append(lines, "+ "+cut(b[j], 500))
			j++
		}
		shown++
	}
	return strings.Join(lines, "\n")
}

func (a *App) revisionChanges(st M, local bool, attach func(string, string) string) string {
	if number(st, "revision", 1) <= 1 {
		return ""
	}
	blocks := []string{"<h2>本版改动</h2>"}
	if request := str(st, "feedback"); request != "" {
		blocks = append(blocks, paragraph("本次修改要求：\n"+request))
	}
	notes := objects(obj(st, "presentation")["revision_notes"])
	if len(notes) == 0 {
		blocks = append(blocks, paragraph("本版未提供逐条修改说明；请结合下面的实际文件差异及第五部分的独立复审核对。"))
	} else {
		items := []string{}
		for _, note := range notes {
			status := map[string]string{"addressed": "已处理", "partial": "部分处理", "unresolved": "未解决"}[str(note, "status")]
			if status == "" {
				status = "待核对"
			}
			items = append(items, "【"+status+"】"+str(note, "request")+"\n"+str(note, "detail")+"\n涉及文件："+strings.Join(texts(note["files"]), "、"))
		}
		blocks = append(blocks, paragraph("以下为主写的修改说明，实际文件变化列在其后；独立复审意见见第五部分。"), ordered(items))
	}
	if pending := texts(st["blockers"]); len(pending) > 0 {
		blocks = append(blocks, paragraph("仍未解决："), ordered(pending))
	}
	previous := M{}
	for _, entry := range objects(st["revision_history"]) {
		r := number(entry, "revision", 0)
		if r < number(st, "revision", 1) && r > number(previous, "revision", 0) {
			previous = entry
		}
	}
	if len(previous) == 0 {
		return strings.Join(append(blocks, paragraph("没有可核对的上版产物记录，无法生成文件差异；从后续版本开始保留对照。")), "\n")
	}
	oldRevision := number(previous, "revision", 1)
	blocks = append(blocks, paragraph(fmt.Sprintf("文件对照：第 %d 版 → 第 %d 版，依据冻结产物的实际内容。", oldRevision, number(st, "revision", 1))))
	if u, e := url.Parse(str(obj(previous, "review_doc"), "url")); e == nil && u.Scheme == "https" && u.Host != "" {
		blocks = append(blocks, `<p><a href="`+esc(u.String())+`">打开上版审阅文档</a></p>`)
	} else if local && str(previous, "local_review") != "" {
		if e := attempt(func() {
			p := inside(a.Root, relative(a.Root, str(previous, "local_review")))
			rel, err := filepath.Rel(a.reviewDir(st), p)
			check(err)
			u := url.URL{Path: filepath.ToSlash(rel)}
			blocks = append(blocks, `<p><a href="`+esc(u.String())+`">打开上版本地审阅页</a></p>`)
		}); e != nil {
			blocks = append(blocks, paragraph("上版本地审阅页已不可用。"))
		}
	}
	before, after := map[string]comparisonFile{}, map[string]comparisonFile{}
	load := func(version M, dest map[string]comparisonFile, old bool) {
		for _, artifact := range objects(version["artifacts"]) {
			p := str(artifact, "path")
			e := attempt(func() {
				p = inside(a.Root, relative(a.Root, p))
				ensure(digest(p) == str(artifact, "sha256"), "历史产物已改变")
				if old {
					blocks = append(blocks, attach(p, fmt.Sprintf("上版 r%d · %s", oldRevision, filepath.Base(p))))
				}
				files := comparisonFiles(p)
				for name, file := range files {
					dest[name] = file
				}
			})
			if e != nil {
				blocks = append(blocks, paragraph("未能对照 "+filepath.Base(p)+"："+safeError(e)))
				// A missing baseline must never be presented as newly added files.
				panic(fmt.Errorf("部分历史文件不可用，本版文件差异未生成；当前产物仍见第三部分"))
			}
		}
	}
	if e := attempt(func() { load(previous, before, true); load(st, after, false) }); e != nil {
		return strings.Join(append(blocks, paragraph(e.Error())), "\n")
	}
	names := map[string]bool{}
	for name := range before {
		names[name] = true
	}
	for name := range after {
		names[name] = true
	}
	keys := []string{}
	for name := range names {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	rows, diffs := []string{}, []string{}
	var detail strings.Builder
	comparedText := 0
	added, changed, removed, unchanged := 0, 0, 0, 0
	for _, name := range keys {
		old, had := before[name]
		current, has := after[name]
		state := "未变化"
		switch {
		case !had:
			state = "新增"
			added++
		case !has:
			state = "删除"
			removed++
		case old.Hash != current.Hash:
			state = "修改"
			changed++
		default:
			unchanged++
		}
		if len(rows) < 20 {
			rows = append(rows, "<tr><td>"+paragraph(name)+"</td><td>"+paragraph(state)+"</td></tr>")
		}
		if state != "未变化" && (!had || old.IsText) && (!has || current.IsText) && comparedText < 6 {
			changes := lineChanges(old.Text, current.Text)
			detail.WriteString(name + "\n" + changes + "\n\n")
			comparedText++
			if len(diffs) < 2 {
				lines := strings.Split(changes, "\n")
				if len(lines) > 25 {
					lines = append(lines[:25], "…更多变化见版本对照附件。")
				}
				diffs = append(diffs, "<h3>"+esc(name)+"</h3><pre lang=\"plain text\"><code>"+esc(strings.Join(lines, "\n"))+"</code></pre>")
			}
		}
	}
	blocks = append(blocks, paragraph(fmt.Sprintf("新增 %d 项 · 修改 %d 项 · 删除 %d 项 · 未变化 %d 项（ZIP 同时列出包内文件）", added, changed, removed, unchanged)), "<table><colgroup><col width=\"580\"/><col width=\"160\"/></colgroup><tr><th><p>文件</p></th><th><p>变化</p></th></tr>"+strings.Join(rows, "")+"</table>")
	if len(keys) > 20 {
		blocks = append(blocks, paragraph("文件较多，表格仅列前 20 项；完整产物见前后版本附件。"))
	}
	if len(diffs) > 0 {
		p := filepath.Join(a.reviewDir(st), "assets", fmt.Sprintf("revision-r%d-r%d.txt", oldRevision, number(st, "revision", 1)))
		writeFile(p, []byte(detail.String()), 0600)
		blocks = append(blocks, paragraph("正文预览至多 2 个文本文件的变化片段；版本对照附件列出至多 6 个文件、每个文件前 80 行变化，长行最多显示 500 字。PDF、Office 等请打开上版附件和第三部分的本版文件对照。"), strings.Join(diffs, "\n"), attach(p, "版本对照.txt"))
	}
	return strings.Join(blocks, "\n")
}
