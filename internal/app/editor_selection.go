package app

import (
	"sort"
	"strings"
	"unicode/utf16"
)

func editorSegments(md string, ranges []M) []M {
	ensure(len(ranges) > 0 && len(ranges) <= 100, "请选择 1 至 100 段正文")
	sort.Slice(ranges, func(i, j int) bool { return number(ranges[i], "start", -1) < number(ranges[j], "start", -1) })
	segments := []M{}
	for _, span := range ranges {
		start, end := number(span, "start", -1), number(span, "end", -1)
		selected := utf16Slice(md, start, end)
		ensure(strings.TrimSpace(selected) != "", "选区不能为空")
		if len(segments) > 0 {
			previous := segments[len(segments)-1]
			last := number(previous, "end", -1)
			ensure(start >= last, "选区不能重叠，请重新选择")
			if strings.TrimSpace(utf16Slice(md, last, start)) == "" {
				previous["end"] = end
				previous["selected"] = utf16Slice(md, number(previous, "start", 0), end)
				continue
			}
		}
		segments = append(segments, M{"id": len(segments) + 1, "start": start, "end": end, "selected": selected})
	}
	total := 0
	for _, segment := range segments {
		total += len(str(segment, "selected"))
	}
	ensure(total <= 32000, "选中文字合计超过 32 KB，请缩小修改范围")
	return segments
}

func editorRewritePrompt(r M) (string, M) {
	prompt := `你是报告的局部编辑。使用当前 CLI 默认模型，不调用其他 agent。
report.md 是全文上下文，input/ 是原题和资料，current-artifacts/ 是当前交付文件。核对涉及的代码或运行方式后再改表述；可以在本目录内解压产物供核对，不修改原始产物。不要生成整份报告、调用提交接口或访问上级目录。保留事实、数据、图片引用和必要格式，不编造结果。选区以外不改；不要在替换内容中附说明或代码围栏（原文是代码块时除外）。
报告正文用简短自然的表达，直接说明用法、算法或结果。核对过程、未运行平台等验证限制写入 summary，不扩写为正文里的自我评价。如果必须改代码才能满足用户要求，在 summary 中说明需要的代码修改，提示通过“提交成品”同步代码；不能声称已改代码或验证了未运行的平台。
`
	segments := objects(r["segments"])
	if len(segments) == 0 {
		return prompt + "只修改选中的原文，返回替换片段 replacement_markdown 和简短说明 summary。\n选中的原文：\n" + str(r, "selected") + "\n用户要求：\n" + str(r, "instruction"), parseMap([]byte(`{"type":"object","additionalProperties":false,"required":["replacement_markdown","summary"],"properties":{"replacement_markdown":{"type":"string"},"summary":{"type":"string"}}}`))
	}
	selected := []M{}
	for _, segment := range segments {
		selected = append(selected, M{"id": segment["id"], "text": segment["selected"]})
	}
	prompt += "用户同时选中了以下片段，请结合全文与全部选区统一修改。相邻段落已合并成同一个片段，可以在片段内合并、删减或调整顺序。不相邻片段之间的未选内容必须保留，不能把它们包含进替换内容。按每个片段的 id 返回 replacements（id 和 replacement_markdown），每个 id 恰好出现一次；无需改动的片段原样返回。summary 简述共同修改，替换片段里不加编号标签或解释。\n选中的片段：\n" + string(jsonBytes(selected)) + "\n用户要求：\n" + str(r, "instruction")
	schema := parseMap([]byte(`{"type":"object","additionalProperties":false,"required":["replacements","summary"],"properties":{"replacements":{"type":"array","items":{"type":"object","additionalProperties":false,"required":["id","replacement_markdown"],"properties":{"id":{"type":"integer"},"replacement_markdown":{"type":"string"}}}},"summary":{"type":"string"}}}`))
	return prompt, schema
}

func storeEditorReplacements(r, result M) {
	segments := objects(r["segments"])
	if len(segments) == 0 {
		replacement, ok := result["replacement_markdown"].(string)
		ensure(ok && len(replacement) <= 64000, "Agent 未返回有效的替换片段")
		r["replacement"] = replacement
		return
	}
	replacements := objects(result["replacements"])
	ensure(len(replacements) == len(segments), "Agent 返回的片段数量与选区不一致，请重试")
	seen := map[int]bool{}
	total := 0
	for _, replacement := range replacements {
		id := number(replacement, "id", -1)
		text, ok := replacement["replacement_markdown"].(string)
		ensure(id > 0 && id <= len(segments) && !seen[id] && ok, "Agent 返回的片段编号或文字无效，请重试")
		total += len(text)
		ensure(total <= 128000 && len(text) <= 64000, "Agent 返回的替换文字过长，请缩小修改范围")
		seen[id] = true
		segments[id-1]["replacement"] = text
	}
	r["segments"] = segments
}

func applyEditorReplacements(md string, r M) string {
	base := str(r, "base_markdown")
	units := utf16.Encode([]rune(base))
	segments := objects(r["segments"])
	if len(segments) == 0 {
		segments = []M{r}
	}
	type edit struct {
		start, end int
		text       string
	}
	edits := []edit{}
	for _, segment := range segments {
		old := str(segment, "selected")
		start, end := number(segment, "start", -1), number(segment, "end", -1)
		ensure(utf16Slice(base, start, end) == old && old != "", "建议选区无效，请重新发起")
		text, ok := segment["replacement"].(string)
		ensure(ok, "此建议缺少替换片段，请重新发起")
		at := len(string(utf16.Decode(units[:start])))
		if md != base {
			ensure(strings.Count(md, old) == 1, "选中的原文已改动或出现多次。保留当前草稿，请重新选择这些段落调用 Agent。")
			before, after := []rune(string(utf16.Decode(units[:start]))), []rune(string(utf16.Decode(units[end:])))
			if len(before) > 60 {
				before = before[len(before)-60:]
			}
			if len(after) > 60 {
				after = after[:60]
			}
			at = strings.Index(md, old)
			ensure(strings.HasSuffix(md[:at], string(before)) && strings.HasPrefix(md[at+len(old):], string(after)), "原段落附近的文字已改变，无法确认修改位置。请重新选择这些段落调用 Agent。")
		}
		edits = append(edits, edit{at, at + len(old), text})
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	next, boundary := md, len(md)
	for _, edit := range edits {
		ensure(edit.end <= boundary, "建议选区发生重叠，请重新选择")
		next = next[:edit.start] + edit.text + next[edit.end:]
		boundary = edit.start
	}
	return next
}
