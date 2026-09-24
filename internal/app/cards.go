package app

import (
	"fmt"
	"html"
	"net/url"
	"strings"
)

func plain(s string) M { return M{"tag": "plain_text", "content": s} }
func md(s string) M    { return M{"tag": "markdown", "content": s} }
func cardEscape(s string) string {
	s = html.EscapeString(s)
	for _, c := range "*`[]_~" {
		s = strings.ReplaceAll(s, string(c), fmt.Sprintf("&#%d;", c))
	}
	return s
}
func columns(items []string, bg string) M {
	cols := []M{}
	for _, s := range items {
		cols = append(cols, M{"tag": "column", "width": "weighted", "weight": 1, "padding": "12px", "background_style": bg, "elements": []M{md(s)}})
	}
	return M{"tag": "column_set", "flex_mode": "none", "horizontal_spacing": "12px", "columns": cols}
}
func panel(title, text string, expanded bool) M {
	return M{"tag": "collapsible_panel", "expanded": expanded, "header": M{"title": plain(title)}, "padding": "12px", "border": M{"color": "grey-200", "corner_radius": "8px"}, "elements": []M{md(text)}}
}
func frame(title, subtitle, color string, elements []M, tag string) M {
	header := M{"title": plain(title), "subtitle": plain(subtitle), "template": color}
	if tag != "" {
		header["text_tag_list"] = []M{{"tag": "text_tag", "text": plain(tag), "color": color}}
	}
	return M{"schema": "2.0", "config": M{"update_multi": true, "width_mode": "default", "enable_forward": false, "summary": M{"content": title + " · " + subtitle}}, "header": header, "body": M{"direction": "vertical", "vertical_spacing": "12px", "padding": "12px", "elements": elements}}
}
func button(label, style string, behavior M) M {
	return M{"tag": "button", "text": plain(label), "type": style, "width": "fill", "behaviors": []M{behavior}}
}
func assignmentCard(st M) M {
	ready := boolean(st, "ready")
	review := obj(st, "review_doc")
	status, color, bg, tag := "需要修改或补充", "orange", "orange-50", "待补充"
	if ready {
		status = "产物待审阅"
		if str(st, "review_outcome") == "approved" {
			status = "独立复审通过 · 待你确认"
		}
		color = "indigo"
		bg = "blue-50"
		tag = "待确认"
	}
	elements := []M{columns([]string{"**" + status + "**\n作业描述、关键结果和所有产物集中在云文档。", fmt.Sprintf("截止时间（北京）\n**%s**\n第 %d 版", cardEscape(str(st, "deadline")), number(st, "revision", 1))}, bg)}
	value := func(action string) M {
		return M{"task_id": st["task_id"], "revision": st["revision"], "nonce": st["nonce"], "action": action, "sha256": st["sha256"]}
	}
	controls := []M{}
	if boolean(review, "verified") && str(review, "url") != "" {
		controls = append(controls, button("打开审阅文档", "primary_filled", M{"type": "open_url", "default_url": review["url"]}), button("按文档批注修改", "default", M{"type": "callback", "value": value("revise_comments")}))
	}
	if ready {
		controls = append(controls, button("审阅完成，提交本版", "default", M{"type": "callback", "value": value("submit")}))
	}
	if len(controls) > 0 {
		cols := []M{}
		for _, b := range controls {
			cols = append(cols, M{"tag": "column", "width": "weighted", "weight": 1, "elements": []M{b}})
		}
		elements = append(elements, M{"tag": "column_set", "flex_mode": "none", "horizontal_spacing": "8px", "columns": cols})
	}
	detail := "在云文档中查看题目、预览报告、下载全部产物。\n\n文档中的批注和编辑不会直接更改提交文件。"
	if len(objects(st["review_history"])) > 0 {
		detail += "\n\n历次独立复审的结论、意见和检查记录都在云文档第五部分。"
	}
	if boolean(st, "source_cached") {
		detail += "\n\n基于已下载资料；当前登录待恢复，尚未重新同步。"
	}
	if blockers := texts(st["blockers"]); len(blockers) > 0 {
		detail += "\n\n**待补充项**"
		for _, s := range blockers {
			detail += "\n• " + cardEscape(s)
		}
	}
	elements = append(elements, panel("审阅说明与待补充项", detail, !ready), M{"tag": "form", "name": "feedback_form", "elements": []M{{"tag": "input", "name": "feedback", "input_type": "multiline_text", "rows": 2, "required": true, "label": plain("直接写修改意见"), "placeholder": plain("例如：报告第 3 页补充推导；案例 4 的输出有问题")}, {"tag": "button", "name": "revise", "form_action_type": "submit", "text": plain("按意见修改"), "type": "default"}, md("<font color=\"grey\">修改后会发送新版本；上版提交按钮随即失效。</font>")}})
	return frame(str(st, "title"), str(st, "course"), color, elements, tag)
}
func receiptCard(title, summary, details string, success bool) M {
	color, bg := "green", "green-50"
	if !success {
		color, bg = "orange", "orange-50"
	}
	elements := []M{columns([]string{"**" + cardEscape(summary) + "**"}, bg)}
	if details != "" {
		elements = append(elements, md(cardEscape(details)))
	}
	return frame(title, "AvatarTHU · 课程助手", color, elements, "")
}
func cut(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}
func noticeCard(n M) M {
	body := cut(str(n, "body"), 3500)
	suffix := ""
	if body != str(n, "body") {
		suffix = "\n\n原文较长，完整内容见网络学堂。"
	}
	if str(n, "attachment_name") != "" {
		suffix += "\n\n公告附件：" + cardEscape(str(n, "attachment_name"))
	}
	link := schoolBase + "/f/wlxt/kcgg/wlkc_ggb/student/beforeViewXs?" + url.Values{"wlkcid": {str(n, "course_id")}, "id": {str(n, "id")}}.Encode()
	return frame("网络学堂 · 今日更新", "课程公告", "turquoise", []M{columns([]string{"**1 条未读公告**\n送达后自动标为已读", cardEscape(str(n, "course"))}, "turquoise-50"), panel(str(n, "course")+" · "+str(n, "title"), "<font color=\"grey\">"+cardEscape(str(n, "date"))+"</font>\n\n"+cardEscape(body)+suffix, true), button("查看公告原文与附件", "primary_filled", M{"type": "open_url", "default_url": link})}, "")
}
