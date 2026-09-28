package app

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"

	"golang.org/x/net/html"
)

const reportStyle = `:root{color-scheme:light;font-family:-apple-system,BlinkMacSystemFont,"Segoe UI","PingFang SC",sans-serif;color:#26232b;background:#f5f4f7}*{box-sizing:border-box}body{margin:0;line-height:1.9;font-size:16px}.layout{max-width:1180px;margin:44px auto;display:grid;grid-template-columns:200px minmax(0,850px);gap:34px;padding:0 24px}nav{position:sticky;top:40px;align-self:start;padding-top:18px;font-size:13px}nav strong{display:block;color:#918696;font-weight:500;font-size:11px;letter-spacing:2px;margin-bottom:15px}nav a{display:block;color:#796683;padding:6px 0;text-decoration:none}nav a:hover{color:#542c71}article{background:#fff;padding:58px 68px;border:1px solid #e8e3ec;border-radius:5px;box-shadow:0 4px 28px #30233b05;min-width:0}h1{font-size:29px;line-height:1.4;margin:0 0 38px;text-align:center;letter-spacing:.4px}h2{font-size:21px;margin:38px 0 18px;scroll-margin-top:24px}h3{font-size:18px;margin:26px 0 12px;scroll-margin-top:24px}p{margin:14px 0}li{margin:9px 0}img{max-width:100%;height:auto;display:block;margin:28px auto 14px;border:1px solid #ece9f0;border-radius:4px}pre{background:#f6f5f8;border:1px solid #eeeaf1;padding:16px 20px;line-height:1.7;overflow:auto;border-radius:6px;font-size:13px}code{font-family:ui-monospace,SFMono-Regular,Consolas,monospace;font-size:.9em}table{border-collapse:collapse;width:100%;font-size:14px;display:block;overflow:auto}th,td{border:1px solid #e4dfeb;padding:8px 13px;text-align:left}th{background:#f6f3f9}blockquote{margin:18px 0;border-left:3px solid #9f7eb6;padding:1px 18px;color:#776981}a{color:#684185}hr{border:0;border-top:1px solid #e8e3ec;margin:30px 0}@media(max-width:900px){.layout{display:block;margin:20px auto;max-width:820px}nav{position:static;padding:0 0 22px}nav a{display:inline-block;margin-right:18px}nav strong{margin-bottom:6px}article{padding:38px}}@media(max-width:520px){.layout{padding:0 12px;margin:12px auto}article{padding:24px;font-size:15px}h1{font-size:23px}h2{font-size:19px}}@media print{@page{size:A4;margin:18mm}body{background:white;font-size:11pt}.layout{display:block;margin:0;padding:0;max-width:none}nav{display:none}article{padding:0;border:0;box-shadow:none}h1{font-size:21pt;margin-bottom:22pt}h2{font-size:15pt;break-after:avoid}h3{break-after:avoid}img,pre,tr{break-inside:avoid}a{color:inherit;text-decoration:none}}`

const reportPolicy = "default-src 'none'; img-src data:; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'"

func standaloneReportHTML(markdown, source, title string) []byte {
	var rendered bytes.Buffer
	check(reportMarkdown.Convert([]byte(markdown), &rendered))
	root := parseHTML(rendered.String())
	headings := []string{}
	imageCache := map[string]string{}
	for _, node := range nodes(root, func(n *html.Node) bool { return n.Type == html.ElementNode }) {
		if node.Data == "h2" || node.Data == "h3" {
			id := fmt.Sprintf("section-%d", len(headings)+1)
			node.Attr = append(node.Attr, html.Attribute{Key: "id", Val: id})
			var label strings.Builder
			for _, n := range nodes(node, func(n *html.Node) bool { return n.Type == html.TextNode }) {
				label.WriteString(n.Data)
			}
			headings = append(headings, `<a href="#`+id+`">`+esc(label.String())+`</a>`)
		}
		for i := range node.Attr {
			attr := &node.Attr[i]
			if node.Data == "img" && attr.Key == "src" {
				original := attr.Val
				if data, ok := imageCache[original]; ok {
					attr.Val = data
					continue
				}
				u, e := url.Parse(original)
				check(e)
				ensure(!u.IsAbs() && u.Host == "" && !strings.HasPrefix(u.Path, "/"), "HTML 审阅稿请使用报告内的相对路径图片："+original)
				ensure(editorImage(u.Path), "HTML 审阅稿暂不支持此图片类型："+original)
				p := inside(source, filepath.FromSlash(u.Path))
				b := readBytes(p)
				ensure(len(b) <= 32<<20, "报告图片超过 32 MB："+original)
				mime := http.DetectContentType(b)
				ensure(strings.HasPrefix(mime, "image/"), "报告图片内容无效："+original)
				attr.Val = "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(b)
				imageCache[original] = attr.Val
			}
			if node.Data == "a" && attr.Key == "href" {
				u, e := url.Parse(attr.Val)
				if e != nil || !(strings.HasPrefix(attr.Val, "#") || u.Scheme == "https" || u.Scheme == "http") {
					attr.Val = "#"
				}
			}
		}
		if node.Data == "a" {
			node.Attr = append(node.Attr, html.Attribute{Key: "rel", Val: "noopener noreferrer"})
		}
	}
	var body bytes.Buffer
	for _, node := range nodes(root, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "body" }) {
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			check(html.Render(&body, child))
		}
	}
	return []byte(`<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="referrer" content="no-referrer"><meta http-equiv="Content-Security-Policy" content="` + esc(reportPolicy) + `"><title>` + esc(title) + `</title><style>` + reportStyle + `</style></head><body><div class="layout"><nav aria-label="报告目录"><strong>报告目录</strong>` + strings.Join(headings, "") + `</nav><article>` + body.String() + `</article></div></body></html>`)
}

func (a *App) exportReportHTML(st M) string {
	draft := a.editorDraft(st)
	ensure(strings.TrimSpace(str(draft, "markdown")) != "", "这份报告还没有 Markdown 正文，请先在编辑页填写")
	b := standaloneReportHTML(str(draft, "markdown"), str(draft, "source_dir"), str(st, "title"))
	target := filepath.Join(a.editorDir(st), "exports", "report.html")
	writeFile(target, b, 0600)
	return target
}

func addReportHTML(result M, job string) {
	files := texts(result["files"])
	if !containsText(files, "final/report.md") {
		return
	}
	source := filepath.Join(job, "html-source")
	mkdir(source)
	if containsText(files, "final/report-source.zip") {
		extractEditorSource(inside(job, "final/report-source.zip"), source)
	}
	copyEditorImages(filepath.Join(job, "final"), source)
	markdown := string(readBytes(inside(job, "final/report.md")))
	writeFile(filepath.Join(job, "final", "report.html"), standaloneReportHTML(markdown, source, "报告审阅稿"), 0600)
	found := false
	for i, file := range files {
		if filepath.Base(file) == "report.html" {
			files[i] = "final/report.html"
			found = true
		}
	}
	if !found {
		files = append(files, "final/report.html")
	}
	result["files"] = files
}
func containsText(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}
