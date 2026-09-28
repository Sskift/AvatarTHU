package app

import (
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
)

func selectionRange(md, selected string) M {
	at := strings.Index(md, selected)
	start := len(utf16.Encode([]rune(md[:at])))
	return M{"start": start, "end": start + len(utf16.Encode([]rune(selected)))}
}

func TestEditorMultiParagraphRequestAndAtomicAdoption(t *testing.T) {
	a, st := editorFixture(t)
	md := "# 报告\n\n先打开🙂程序。\n\n再输入多边形。\n\n![截图保持](screenshots/example.png)\n\n点击运行，查看结果。\n\n最后一句保持。\n"
	a.saveEditorDraft(st, M{"version": 1, "markdown": md})
	id := strings.Repeat("7", 32)
	path := "/api/task/" + str(st, "task_id")
	before := digest(str(st, "submission"))
	in := M{"id": id, "version": 2, "instruction": "统一简化操作步骤", "ranges": []M{
		selectionRange(md, "点击运行，查看结果。"), selectionRange(md, "先打开🙂程序。"), selectionRange(md, "再输入多边形。"),
	}}
	r := editorResult(t, editorCall(t, a, path+"/rewrite", in))
	segments := objects(r["segments"])
	if len(segments) != 2 || str(segments[0], "selected") != "先打开🙂程序。\n\n再输入多边形。" {
		t.Fatal("adjacent paragraphs were not grouped", segments)
	}
	calls := 0
	a.RunModel = func(tool, job, prompt string, schema M, role string, plan M) M {
		calls++
		if !strings.Contains(prompt, "统一简化操作步骤") || !strings.Contains(prompt, "点击运行，查看结果。") || !strings.Contains(prompt, "先打开🙂程序。") || obj(schema, "properties")["replacements"] == nil {
			t.Fatal("selected paragraphs were not provided together")
		}
		return M{"replacements": []M{{"id": 2, "replacement_markdown": "点击运行查看结果。"}, {"id": 1, "replacement_markdown": "打开程序并输入多边形。"}}, "summary": "统一操作表述"}
	}
	a.processEditorRequests("")
	if calls != 1 || str(a.editorDraft(st), "markdown") != md {
		t.Fatal("multi-paragraph edit did not use one session or changed draft early")
	}
	requestPath := filepath.Join(a.editorDir(st), "requests", id, "request.json")
	if str(readMap(requestPath), "state") != "ready" {
		t.Fatal(readMap(requestPath))
	}
	changed := strings.Replace(md, "点击运行，查看结果。", "我改了结果这一段。", 1)
	a.saveEditorDraft(st, M{"version": 2, "markdown": changed})
	if res := editorCall(t, a, path+"/decide", M{"id": id, "action": "accept"}); res.Code != 409 {
		t.Fatal("partially applied a suggestion after a selected paragraph changed")
	}
	if str(a.editorDraft(st), "markdown") != changed {
		t.Fatal("conflict overwrote another selected paragraph")
	}
	a.saveEditorDraft(st, M{"version": 3, "markdown": md})
	editorResult(t, editorCall(t, a, path+"/decide", M{"id": id, "action": "accept"}))
	expected := strings.Replace(strings.Replace(md, "先打开🙂程序。\n\n再输入多边形。", "打开程序并输入多边形。", 1), "点击运行，查看结果。", "点击运行查看结果。", 1)
	if str(a.editorDraft(st), "markdown") != expected || digest(str(st, "submission")) != before {
		t.Fatal("unselected text, images, or frozen artifacts changed")
	}
	editorResult(t, editorCall(t, a, path+"/decide", M{"id": id, "action": "accept"}))
	if str(a.editorDraft(st), "markdown") != expected {
		t.Fatal("duplicate adoption applied twice")
	}
}

func TestEditorMultiParagraphValidationAndConcurrentOutsideEdit(t *testing.T) {
	md := "第一段🙂。\n\n" + strings.Repeat("中间保留。", 40) + "\n\n第三段。\n\n" + strings.Repeat("尾部保留。", 40)
	first, last := selectionRange(md, "第一段🙂。"), selectionRange(md, "第三段。")
	for _, ranges := range [][]M{nil, {first, first}, {{"start": 4, "end": 5}}, {{"start": 0, "end": 10000}}} {
		if e := attempt(func() { editorSegments(md, ranges) }); e == nil {
			t.Fatal("accepted invalid selection", ranges)
		}
	}
	segments := editorSegments(md, []M{first, last})
	for _, replacements := range [][]M{
		{{"id": 1, "replacement_markdown": "改一"}},
		{{"id": 1, "replacement_markdown": "改一"}, {"id": 1, "replacement_markdown": "改三"}},
		{{"id": 1, "replacement_markdown": "改一"}, {"id": 3, "replacement_markdown": "改三"}},
	} {
		if e := attempt(func() { storeEditorReplacements(M{"segments": segments}, M{"replacements": replacements}) }); e == nil {
			t.Fatal("accepted incomplete replacements")
		}
	}
	r := M{"base_markdown": md, "segments": editorSegments(md, []M{first, last})}
	storeEditorReplacements(r, M{"replacements": []M{{"id": 1, "replacement_markdown": "改一"}, {"id": 2, "replacement_markdown": "改三"}}})
	current := md + "\n\n另外补充。"
	expected := strings.Replace(strings.Replace(current, "第一段🙂。", "改一", 1), "第三段。", "改三", 1)
	if got := applyEditorReplacements(current, r); got != expected {
		t.Fatal("lost unrelated concurrent edits", got)
	}
}
