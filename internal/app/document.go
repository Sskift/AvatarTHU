package app

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	_ "golang.org/x/image/webp"
	stdhtml "html"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"golang.org/x/net/html"
)

func esc(s string) string       { return stdhtml.EscapeString(s) }
func paragraph(s string) string { return "<p>" + strings.ReplaceAll(esc(s), "\n", "<br/>") + "</p>" }
func ordered(items []string) string {
	if len(items) == 0 {
		return ""
	}
	r := "<ol>"
	for _, s := range items {
		r += "<li>" + strings.ReplaceAll(esc(s), "\n", "<br/>") + "</li>"
	}
	return r + "</ol>"
}
func prose(source string) string {
	source, formulas := protectMath(source)
	var b bytes.Buffer
	check(goldmark.New(goldmark.WithExtensions(extension.GFM)).Convert([]byte(source), &b))
	root := parseHTML(b.String())
	var convert func(*html.Node) string
	minimum := 6
	for _, n := range nodes(root, func(n *html.Node) bool {
		return len(n.Data) == 2 && n.Data[0] == 'h' && n.Data[1] >= '1' && n.Data[1] <= '6'
	}) {
		if int(n.Data[1]-'0') < minimum {
			minimum = int(n.Data[1] - '0')
		}
	}
	previous := 1
	convert = func(n *html.Node) string {
		if n.Type == html.TextNode {
			return esc(n.Data)
		}
		if n.Type != html.ElementNode && n.Type != html.DocumentNode {
			return ""
		}
		tag := n.Data
		var inner strings.Builder
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			inner.WriteString(convert(c))
		}
		content := inner.String()
		if len(tag) == 2 && tag[0] == 'h' && tag[1] >= '1' && tag[1] <= '6' {
			level := 2 + int(tag[1]-'0') - minimum
			if level > previous+1 {
				level = previous + 1
			}
			if level > 6 {
				level = 6
			}
			previous = level
			return fmt.Sprintf("<h%d>%s</h%d>", level, content, level)
		}
		switch tag {
		case "strong":
			return "<b>" + content + "</b>"
		case "em", "del", "p", "ol", "ul", "li", "blockquote", "tr", "thead", "tbody":
			return "<" + tag + ">" + content + "</" + tag + ">"
		case "br", "hr":
			return "<" + tag + "/>"
		case "a":
			u := attr(n, "href")
			if strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "http://") {
				return `<a href="` + esc(u) + `">` + content + `</a>`
			}
			return content
		case "img":
			return esc(attr(n, "alt"))
		case "code":
			if n.Parent != nil && n.Parent.Data == "pre" {
				return "<code>" + content + "</code>"
			}
			return `<span background-color="light-gray">` + content + "</span>"
		case "pre":
			lang := "plain text"
			if n.FirstChild != nil {
				c := attr(n.FirstChild, "class")
				if strings.HasPrefix(c, "language-") {
					lang = strings.TrimPrefix(c, "language-")
				}
			}
			return `<pre lang="` + esc(lang) + `">` + content + `</pre>`
		case "td", "th":
			color := ""
			if tag == "th" {
				color = ` background-color="light-gray"`
			}
			return "<" + tag + color + "><p>" + content + "</p></" + tag + ">"
		case "table":
			count := 0
			rows := nodes(n, func(x *html.Node) bool { return x.Data == "tr" })
			if len(rows) > 0 {
				for c := rows[0].FirstChild; c != nil; c = c.NextSibling {
					if c.Data == "th" || c.Data == "td" {
						count++
					}
				}
			}
			cols := "<colgroup>"
			if count > 0 {
				for i := 0; i < count; i++ {
					cols += fmt.Sprintf(`<col width="%d"/>`, max(80, 820/count))
				}
			}
			return "<table>" + cols + "</colgroup>" + content + "</table>"
		default:
			return content
		}
	}
	result := convert(root)
	for token, formula := range formulas {
		result = strings.ReplaceAll(result, token, "<latex>"+esc(formula)+"</latex>")
	}
	return result
}

func protectMath(source string) (string, map[string]string) {
	formulas := map[string]string{}
	var out strings.Builder
	for i := 0; i < len(source); {
		if source[i] == '`' {
			j := i
			for j < len(source) && source[j] == '`' {
				j++
			}
			delimiter := source[i:j]
			if end := strings.Index(source[j:], delimiter); end >= 0 {
				end += j + len(delimiter)
				out.WriteString(source[i:end])
				i = end
				continue
			}
		}
		if source[i] == '$' && (i == 0 || source[i-1] != '\\') {
			n := 1
			if i+1 < len(source) && source[i+1] == '$' {
				n = 2
			}
			delimiter := strings.Repeat("$", n)
			if end := strings.Index(source[i+n:], delimiter); end > 0 {
				end += i + n
				value := source[i+n : end]
				if n == 2 || !strings.Contains(value, "\n") {
					token := "AVATARMATH" + randomID(8)
					formulas[token] = strings.TrimSpace(value)
					out.WriteString(token)
					i = end + n
					continue
				}
			}
		}
		out.WriteByte(source[i])
		i++
	}
	return out.String(), formulas
}
func isImage(p string) bool {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".png", ".jpg", ".jpeg", ".webp":
		return true
	}
	return false
}
func previewImages(path string) map[string][]byte {
	out := map[string][]byte{}
	z, e := zip.OpenReader(path)
	check(e)
	defer z.Close()
	used := uint64(0)
	for _, f := range z.File {
		parts := strings.Split(f.Name, "/")
		safe := !strings.HasPrefix(f.Name, "/") && !strings.Contains(f.Name, `\`) && !strings.Contains(f.Name, ":") && f.Mode()&os.ModeSymlink == 0 && !f.FileInfo().IsDir()
		for _, p := range parts {
			safe = safe && !strings.HasPrefix(p, ".")
		}
		if !safe || !isImage(f.Name) || f.UncompressedSize64 > 4<<20 || used+f.UncompressedSize64 > 24<<20 {
			continue
		}
		r, e := f.Open()
		check(e)
		b, e := io.ReadAll(io.LimitReader(r, (4<<20)+1))
		r.Close()
		check(e)
		ensure(len(b) <= 4<<20, "图片超出预览大小")
		used += uint64(len(b))
		out[f.Name] = b
	}
	return out
}
func imageData(path, member string) []byte {
	var b []byte
	if member != "" {
		ensure(strings.ToLower(filepath.Ext(path)) == ".zip" && isImage(member), "关键结果必须引用 ZIP 中的图片")
		b = previewImages(path)[member]
		ensure(len(b) > 0, "关键结果图片不存在或过大："+member)
	} else {
		ensure(isImage(path), "关键结果必须是图片")
		f, e := os.Open(path)
		check(e)
		defer f.Close()
		b, e = io.ReadAll(io.LimitReader(f, (4<<20)+1))
		check(e)
		ensure(len(b) <= 4<<20, "图片超过 4 MB 预览上限")
	}
	cfg, _, e := image.DecodeConfig(bytes.NewReader(b))
	check(e)
	ensure(cfg.Width > 0 && cfg.Height > 0 && int64(cfg.Width)*int64(cfg.Height) <= 30_000_000, "图片尺寸过大")
	return b
}
func (a *App) reviewDir(st M) string {
	base := a.data("reviews", str(st, "task_id"))
	if d := str(st, "assignment_dir"); d != "" {
		base = filepath.Join(d, "reviews")
	}
	return filepath.Join(base, fmt.Sprintf("r%d", number(st, "revision", 1)))
}
func (a *App) buildDocument(st M, draft string, local bool) M {
	base := a.outputDir(st)
	paths := []string{}
	for _, item := range objects(st["artifacts"]) {
		p := inside(base, relative(base, str(item, "path")))
		ensure(digest(p) == str(item, "sha256"), "审阅产物与冻结版本不一致")
		paths = append(paths, p)
	}
	submission := str(st, "submission")
	if submission != "" {
		submission = inside(base, relative(base, submission))
		ensure(digest(submission) == str(st, "sha256"), "提交包与冻结版本不一致")
	}
	report := inside(base, relative(base, str(st, "report")))
	ensure(str(st, "report_sha256") == "" || digest(report) == str(st, "report_sha256"), "执行自查与冻结版本不一致")
	assets := filepath.Join(filepath.Dir(draft), "assets")
	mkdir(assets)
	names := []string{}
	attach := func(p, name string) string {
		if name == "" {
			name = filepath.Base(p)
		}
		names = append(names, name)
		return `<figure view-type="Card"><source path="@./` + esc(filepath.ToSlash(relative(a.Root, p))) + `" name="` + esc(name) + `"/></figure>`
	}
	pres := obj(st, "presentation")
	blocks := []string{"<title>" + esc(str(st, "title")+" · 审阅") + "</title>", paragraph(str(st, "course") + " · 截止 " + str(st, "deadline") + "（北京时间）"), `<callout emoji="📦" background-color="light-blue">` + paragraph(fmt.Sprintf("本版有 %d 份交付文件，完整产物集中在第三部分。请对照原题审阅；待补充事项见第二部分。", len(paths))) + "</callout>", a.revisionChanges(st, local, attach), "<h1>一、作业描述</h1>"}
	assignment := strDefault(st, "description", "未提供文字描述，请查看原题附件。")
	blocks = append(blocks, prose(cut(assignment, 6000)))
	if cut(assignment, 6000) != assignment {
		p := filepath.Join(assets, "assignment-description.txt")
		writeFile(p, []byte(assignment), 0600)
		blocks = append(blocks, attach(p, "作业描述全文.txt"))
	}
	if job := str(st, "job"); job != "" {
		originals := visibleFiles(filepath.Join(job, "input", "attachments"))
		attachments := []string{}
		for _, p := range originals {
			if strings.HasSuffix(p, ".txt") && exists(strings.TrimSuffix(p, ".txt")) {
				continue
			}
			if regexp.MustCompile(`(?i)\.(pdf|pptx|docx)-page-\d+\.png$`).MatchString(p) {
				continue
			}
			attachments = append(attachments, attach(p, ""))
		}
		if len(attachments) > 0 {
			blocks = append(blocks, "<h2>原题与附件</h2>")
			blocks = append(blocks, attachments...)
		}
	}
	blocks = append(blocks, "<h1>二、完成情况与关键结果</h1>", paragraph("摘要和执行自查来自主写记录，独立复审意见见第五部分。"), prose(cut(str(st, "summary"), 2400)))
	if str(st, "feedback") != "" {
		blocks = append(blocks, "<h2>本版修改要求</h2>", paragraph(str(st, "feedback")))
	}
	if bs := texts(st["blockers"]); len(bs) > 0 {
		blocks = append(blocks, "<h2>待补充事项</h2>", ordered(bs))
	}
	points := objects(pres["highlights"])
	if len(points) == 0 {
		for _, p := range paths {
			if isImage(p) && !strings.Contains(strings.ToLower(p), "mock") && !strings.Contains(strings.ToLower(p), "gui_interface") {
				points = append(points, M{"artifact": filepath.Base(p), "member": "", "title": filepath.Base(p), "detail": "本版实际产物图，请结合原题核对。"})
			} else if strings.ToLower(filepath.Ext(p)) == ".zip" {
				pictures := previewImages(p)
				keys := []string{}
				for key := range pictures {
					keys = append(keys, key)
				}
				sort.Strings(keys)
				for _, key := range keys {
					if !strings.Contains(strings.ToLower(key), "mock") && !strings.Contains(strings.ToLower(key), "gui_interface") {
						points = append(points, M{"artifact": filepath.Base(p), "member": key, "title": filepath.Base(key), "detail": "来自本版交付文件的结果图。"})
					}
				}
			}
		}
	}
	imageCount := 0
	for _, point := range points {
		if imageCount == 3 {
			break
		}
		var file string
		for _, p := range paths {
			if filepath.Base(p) == filepath.Base(str(point, "artifact")) {
				file = p
				break
			}
		}
		ensure(file != "", "关键图未引用本版产物")
		member := str(point, "member")
		b := imageData(file, member)
		sourceName := file
		if member != "" {
			sourceName = member
		}
		imageCount++
		dest := filepath.Join(assets, fmt.Sprintf("figure-%d%s", imageCount, strings.ToLower(filepath.Ext(sourceName))))
		writeFile(dest, b, 0600)
		label := str(point, "detail")
		if strings.Contains(strings.ToLower(sourceName), "mock") || strings.Contains(strings.ToLower(sourceName), "gui_interface") {
			label += "（界面示意图，不作为真实运行截图）"
		}
		blocks = append(blocks, "<h2>"+esc(str(point, "title"))+"</h2>", paragraph(label), `<img path="@./`+esc(filepath.ToSlash(relative(a.Root, dest)))+`" width="820"/>`)
	}
	if imageCount == 0 {
		for _, p := range paths {
			ext := strings.ToLower(filepath.Ext(p))
			if ext == ".md" || ext == ".txt" {
				b, e := os.ReadFile(p)
				check(e)
				blocks = append(blocks, "<h2>答案节选</h2>", prose(cut(string(b), 2400)))
				break
			}
		}
	}
	if checks := texts(pres["checks"]); len(checks) > 0 {
		blocks = append(blocks, "<h2>执行自查与待核对点</h2>", ordered(checks))
	}
	blocks = append(blocks, "<h1>三、完整产物</h1>", paragraph("所有文件集中在本节。点击原生附件可预览或下载；PDF 保留原始排版，代码 ZIP 保留目录结构。"))
	if submission != "" {
		blocks = append(blocks, "<h2>本版提交文件</h2>", attach(submission, ""))
	}
	groups := map[string][]string{"报告与说明": {}, "代码、程序及其他产物": {}}
	for _, p := range paths {
		if p == submission {
			continue
		}
		group := "代码、程序及其他产物"
		switch strings.ToLower(filepath.Ext(p)) {
		case ".pdf", ".docx", ".pptx", ".xlsx", ".md", ".txt":
			group = "报告与说明"
		}
		groups[group] = append(groups[group], p)
	}
	for _, title := range []string{"报告与说明", "代码、程序及其他产物"} {
		if len(groups[title]) > 0 {
			blocks = append(blocks, "<h2>"+title+"</h2>")
			for _, p := range groups[title] {
				blocks = append(blocks, attach(p, ""))
			}
		}
	}
	blocks = append(blocks, "<h2>执行自查记录</h2>", paragraph("由主写会话生成，包含实际命令、结果和限制。"), attach(report, ""), "<h1>四、审阅与操作</h1>")
	instructions := []string{"对照第一部分的原题与附件，确认完成范围。", "检查关键结果，并打开第三部分的报告、代码和自查记录。", "在相关段落或图片上批注，再回卡片点“按文档批注修改”；也可直接在卡片填写意见。", "确认后在当前版本卡片选择提交。直接编辑云文档不会修改待提交文件。"}
	if local {
		instructions[2] = "运行 avatarthu revise " + str(st, "task_id") + ` --feedback "修改意见"，下一次调度生成新版。`
		instructions[3] = "本地模式请自行在网络学堂网页提交；启用飞书后可通过本人当前版本卡片决定提交。"
	}
	blocks = append(blocks, ordered(instructions), "<h1>五、历次独立复审</h1>", paragraph("主写和复审分别使用新进程与新会话，各用 CLI 默认模型。复审只接收原题和当前候选产物，不传递主写对话、自查或以前的复审结论。历次意见如下；最终提交仍由本人决定。"))
	history := objects(st["review_history"])
	if len(history) == 0 {
		blocks = append(blocks, paragraph("本版尚无独立复审记录，请本人核对。"))
	}
	for _, entry := range history {
		verdict := "不通过，需要修改"
		if boolean(entry, "approved") {
			verdict = "通过"
		}
		blocks = append(blocks, fmt.Sprintf("<h2>第 %d 版 · 第 %d 轮 · %s</h2>", number(entry, "revision", 1), number(entry, "round", 1), verdict), paragraph("主写："+str(entry, "writer")+" / 复审："+str(entry, "reviewer")+" / 时间："+str(entry, "reviewed_at")), prose(str(entry, "summary")))
		comments := []string{}
		for _, c := range objects(entry["comments"]) {
			comments = append(comments, str(c, "location")+"\n"+str(c, "comment")+"\n修改建议："+str(c, "suggestion"))
		}
		blocks = append(blocks, ordered(comments))
		if checks := texts(entry["checks"]); len(checks) > 0 {
			blocks = append(blocks, paragraph("复审实际检查："), ordered(checks))
		}
		if limits := texts(entry["limitations"]); len(limits) > 0 {
			blocks = append(blocks, paragraph("仍需核对："), ordered(limits))
		}
	}
	marker := fmt.Sprintf("版本标识：%s / r%d / %s", str(st, "task_id"), number(st, "revision", 1), strDefault(st, "sha256", "无提交包"))
	blocks = append(blocks, paragraph(marker))
	writeFile(draft, []byte(strings.Join(blocks, "\n")), 0600)
	return M{"image_count": imageCount, "has_bundle": submission != "", "pages": 0, "pictures": imageCount, "attachment_names": names, "marker": marker, "draft": draft}
}
func (a *App) publishLocal(st M) {
	dir := a.reviewDir(st)
	draft := filepath.Join(dir, "local.xml")
	a.buildDocument(st, draft, true)
	b, e := os.ReadFile(draft)
	check(e)
	content := strings.ReplaceAll(strings.ReplaceAll(string(b), "<title>", "<h1>"), "</title>", "</h1>")
	root := parseHTML(content)
	for _, n := range nodes(root, func(n *html.Node) bool { return n.Data == "img" || n.Data == "source" }) {
		raw := strings.TrimPrefix(attr(n, "path"), "@./")
		target := filepath.Join(a.Root, filepath.FromSlash(raw))
		rel, e := filepath.Rel(dir, target)
		check(e)
		u := url.URL{Path: filepath.ToSlash(rel)}
		if n.Data == "img" {
			n.Attr = []html.Attribute{{Key: "src", Val: u.String()}, {Key: "loading", Val: "lazy"}, {Key: "alt", Val: filepath.Base(target)}}
		} else {
			label := attr(n, "name")
			if label == "" {
				label = filepath.Base(target)
			}
			n.Data = "a"
			n.Attr = []html.Attribute{{Key: "href", Val: u.String()}, {Key: "class", Val: "attachment"}}
			n.AppendChild(&html.Node{Type: html.TextNode, Data: label})
		}
	}
	for _, n := range nodes(root, func(n *html.Node) bool { return n.Data == "callout" }) {
		n.Data = "aside"
	}
	var out bytes.Buffer
	for _, body := range nodes(root, func(n *html.Node) bool { return n.Data == "body" }) {
		for child := body.FirstChild; child != nil; child = child.NextSibling {
			check(html.Render(&out, child))
		}
	}
	page := `<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src 'self' file: data:; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'"><meta name="viewport" content="width=device-width,initial-scale=1"><title>AvatarTHU 审阅</title><style>body{font:16px/1.8 system-ui,sans-serif;max-width:960px;margin:48px auto;padding:0 24px;color:#243041;background:#f8fafc}h1{font-size:26px;border-bottom:1px solid #dce3ed;padding-bottom:12px;margin-top:40px}h2{font-size:20px}p,li{max-width:85ch}aside,figure{display:block;background:white;padding:16px 20px;border:1px solid #dce3ed;border-radius:10px;margin:16px 0}a{color:#315bc8;overflow-wrap:anywhere}img{max-width:100%;height:auto;border-radius:8px}pre{overflow:auto;background:#eaf0f6;padding:16px}table{border-collapse:collapse;width:100%}td,th{border:1px solid #dce3ed;padding:8px}li+li{margin-top:8px}</style></head><body>` + out.String() + `</body></html>`
	path := filepath.Join(dir, "index.html")
	writeFile(path, []byte(page), 0600)
	st["local_review"] = path
	a.saveTask(st)
}
func (a *App) publishCloud(st M) M {
	review := obj(st, "review_doc")
	st["review_doc"] = review
	if boolean(review, "verified") {
		return review
	}
	if str(review, "draft") == "" {
		decision := M{"audience": "作业提交者", "reader_task": "对照题目审阅当前版本并决定提交或修改", "genre_contract": nil, "adapter": nil, "presentation_mode": "rich", "visual_plan": M{"reason": "按题目、关键结果、完整产物、操作和复审记录展示", "blocks": []any{}}}
		workspace := a.lark("docs", "+script", "--command", "init-draft", "--presentation-decision", string(jsonBytes(decision)))
		draft := str(workspace, "draft_path")
		if !filepath.IsAbs(draft) {
			draft = filepath.Join(str(workspace, "cwd"), draft)
		}
		relative(a.Root, draft)
		folder := filepath.Join(a.reviewDir(st), "cloud")
		if _, e := os.Stat(folder); os.IsNotExist(e) {
			mkdir(filepath.Dir(folder))
			check(os.Rename(filepath.Dir(draft), folder))
		}
		draft = filepath.Join(folder, filepath.Base(draft))
		merge(review, a.buildDocument(st, draft, false))
		a.saveTask(st)
	}
	arg := "@" + filepath.ToSlash(relative(a.Root, str(review, "draft")))
	if str(review, "document_id") == "" {
		ensure(!boolean(review, "creating"), "审阅文档创建结果待核对，请先核对飞书回执，避免重复创建")
		r := a.lark("docs", "+script", "--command", "parse", "--content", arg)
		ensure(str(obj(r, "assessment"), "status") == "passed", "飞书文档解析未通过，请检查草稿日志")
		review["creating"] = true
		a.saveTask(st)
		r = a.lark("docs", "+create", "--as", "user", "--doc-format", "xml", "--content", arg)
		merge(review, obj(r, "document"))
		review["warnings"] = r["warnings"]
		review["written"] = true
		a.saveTask(st)
	}
	if !boolean(review, "written") {
		previous := a.lark("docs", "+fetch", "--as", "user", "--doc", str(review, "document_id"), "--detail", "with-ids")
		if !cloudContainsArtifacts(str(obj(previous, "document"), "content"), review) {
			backup := filepath.Join(filepath.Dir(str(review, "draft")), "previous-cloud.json")
			if !exists(backup) {
				writeJSON(backup, previous)
			}
			parsed := a.lark("docs", "+script", "--command", "parse", "--content", arg)
			ensure(str(obj(parsed, "assessment"), "status") == "passed", "飞书文档解析未通过，请检查草稿日志")
			updated := a.lark("docs", "+update", "--as", "user", "--doc", str(review, "document_id"), "--command", "overwrite", "--doc-format", "xml", "--content", arg)
			ensure(str(updated, "result") == "success", "审阅文档未完整更新，请检查飞书回执")
			review["warnings"] = updated["warnings"]
		}
		review["written"] = true
		a.saveTask(st)
	}
	fetched := a.lark("docs", "+fetch", "--as", "user", "--doc", str(review, "document_id"), "--detail", "with-ids")
	writeJSON(filepath.Join(filepath.Dir(str(review, "draft")), "published.json"), fetched)
	content := str(obj(fetched, "document"), "content")
	warnings := review["warnings"]
	emptyWarnings := warnings == nil || string(jsonBytes(warnings)) == "[]"
	ensure(cloudContainsArtifacts(content, review) && emptyWarnings, "审阅文档发布不完整，已保留回执，请修复后重试")
	review["verified"] = true
	a.saveTask(st)
	return review
}

func cloudContainsArtifacts(content string, review M) bool {
	if !strings.Contains(content, str(review, "marker")) {
		return false
	}
	counts := map[string]int{}
	images := 0
	dec := xml.NewDecoder(strings.NewReader("<root>" + content + "</root>"))
	for {
		t, e := dec.Token()
		if e == io.EOF {
			break
		}
		check(e)
		if node, ok := t.(xml.StartElement); ok {
			if node.Name.Local == "img" {
				images++
			}
			if node.Name.Local == "source" {
				name, token := "", ""
				for _, a := range node.Attr {
					if a.Name.Local == "name" {
						name = a.Value
					}
					if a.Name.Local == "token" {
						token = a.Value
					}
				}
				if token != "" {
					counts[name]++
				}
			}
		}
	}
	for _, n := range texts(review["attachment_names"]) {
		if counts[n] == 0 {
			return false
		}
		counts[n]--
	}
	return images >= number(review, "image_count", 0)
}
func (a *App) comments(st M) []M {
	link := str(obj(st, "review_doc"), "url")
	owner := str(a.config(), "lark_user_id")
	entries := []M{}
	for _, item := range a.pagedLark("drive", "+list-comments", "--as", "user", "--url", link, "--solved-status", "false", "--need-relation", "--page-size", "100") {
		replies := objects(obj(item, "reply_list")["replies"])
		if boolean(item, "has_more") {
			replies = a.pagedLark("drive", "+list-replies", "--as", "user", "--url", link, "--comment-id", str(item, "comment_id"), "--page-size", "100")
		}
		for _, r := range replies {
			if str(r, "user_id") != owner {
				continue
			}
			var text strings.Builder
			for _, part := range objects(obj(r, "content")["elements"]) {
				switch str(part, "type") {
				case "text_run":
					text.WriteString(str(obj(part, "text_run"), "text"))
				case "text":
					if m, ok := part["text"].(M); ok {
						text.WriteString(str(m, "text"))
					} else {
						text.WriteString(str(part, "text"))
					}
				}
			}
			if strings.TrimSpace(text.String()) != "" {
				entries = append(entries, M{"comment_id": item["comment_id"], "reply_id": r["reply_id"], "quote": str(item, "quote"), "text": strings.TrimSpace(text.String()), "relation": item["relation"]})
			}
		}
	}
	needContext := false
	for _, entry := range entries {
		needContext = needContext || len(obj(entry, "relation")) > 0
	}
	if needContext {
		fetched := a.lark("docs", "+fetch", "--as", "user", "--doc", link, "--detail", "with-ids")
		root := parseHTML(str(obj(fetched, "document"), "content"))
		blocks := nodes(root, func(n *html.Node) bool { return n.Type == html.ElementNode })
		for _, entry := range entries {
			relation := obj(entry, "relation")
			if boolean(relation, "content_deleted") {
				entry["context"] = "批注引用内容已删除，请结合原文引用核对"
				continue
			}
			locations := obj(relation, "relation")
			if raw, ok := relation["relation"].(string); ok {
				_ = attempt(func() { locations = parseMap([]byte(raw)) })
			}
			for _, raw := range locations {
				location, ok := raw.(M)
				if !ok {
					continue
				}
				id := str(obj(location, "positionInfo"), "blockID")
				if id == "" {
					continue
				}
				heading, previous := "", ""
				for _, block := range blocks {
					if attr(block, "id") == id {
						value := heading + "\n"
						if block.Data == "img" {
							value += previous + "\n"
						}
						value += cut(textContent(block, " "), 3500)
						for _, f := range nodes(block, func(n *html.Node) bool { return n.Data == "source" }) {
							value += "\n附件：" + attr(f, "name")
						}
						entry["context"] = strings.TrimSpace(value)
						break
					}
					if block.Data == "h1" || block.Data == "h2" {
						heading = textContent(block, " ")
					}
					if block.Data == "p" {
						previous = textContent(block, " ")
					}
				}
				if str(entry, "context") != "" {
					break
				}
			}
		}
	}
	return entries
}
