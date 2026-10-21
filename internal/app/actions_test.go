package app

import (
	"io"
	"net/http"
	"path/filepath"
	"testing"
)

func TestUploadLeavesHomeworkContentEmpty(t *testing.T) {
	a := testApp(t)
	file := filepath.Join(a.Root, "hw2.zip")
	writeFile(file, []byte("approved attachment"), 0600)
	calls := 0
	school := fakeSchool(a, func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != "POST" || r.URL.Path != "/b/wlxt/kczy/zy/student/tjzy" {
			t.Fatalf("unexpected upload: %s %s", r.Method, r.URL.Path)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		defer r.MultipartForm.RemoveAll()
		values := r.MultipartForm.Value["zynr"]
		if len(values) != 1 || values[0] != "" {
			t.Fatalf("homework content must be explicitly empty: %q", values)
		}
		if r.FormValue("xszyid") != "assignment-id" || r.FormValue("isDeleted") != "0" {
			t.Fatal("missing upload metadata")
		}
		f, header, err := r.FormFile("fileupload")
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		body, err := io.ReadAll(f)
		if err != nil || header.Filename != "hw2.zip" || string(body) != "approved attachment" {
			t.Fatal("attachment changed")
		}
		return response(r, `{"result":"success"}`, "application/json", -1), nil
	})
	if receipt := uploadApproved(school, M{"xszyid": "assignment-id"}, file); str(receipt, "result") != "success" || calls != 1 {
		t.Fatalf("unexpected upload receipt or request count: %v, %d", receipt, calls)
	}
}
