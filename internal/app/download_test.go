package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type interruptedBody struct{ reader *strings.Reader }

func (b interruptedBody) Read(p []byte) (int, error) {
	n, err := b.reader.Read(p)
	if err == io.EOF {
		err = io.ErrUnexpectedEOF
	}
	return n, err
}
func (b interruptedBody) Close() error { return nil }

func TestInterruptedDownloadRetriesWithoutReplacingGoodFile(t *testing.T) {
	a := testApp(t)
	target := filepath.Join(a.Root, "lecture.pdf")
	writeFile(target, []byte("previous"), 0600)
	calls := 0
	s := fakeSchool(a, func(r *http.Request) (*http.Response, error) {
		calls++
		previous, _ := os.ReadFile(target)
		if !bytes.Equal(previous, []byte("previous")) {
			t.Fatal("failed attempt replaced the previous file")
		}
		if calls == 1 {
			resp := response(r, "", "application/pdf", 12)
			resp.Body = interruptedBody{strings.NewReader("partial")}
			return resp, nil
		}
		return response(r, "%PDF-complete", "application/pdf", 13), nil
	})
	s.download("/download", target, "new")
	if calls != 2 || str(readMap(filepath.Join(a.Root, ".lecture.pdf.download.json")), "sha256") != digest(target) {
		t.Fatal("retry did not finish and record the full download")
	}
	if temps, _ := filepath.Glob(filepath.Join(a.Root, ".download-*")); len(temps) != 0 {
		t.Fatal("partial files left behind", temps)
	}
}

func TestInterruptedDownloadReportsFileAndDoesNotRetryCancellation(t *testing.T) {
	a := testApp(t)
	ctx, cancel := context.WithCancel(a.Ctx)
	defer cancel()
	a.Ctx = ctx
	calls := 0
	s := fakeSchool(a, func(r *http.Request) (*http.Response, error) {
		calls++
		cancel()
		resp := response(r, "", "application/pdf", 12)
		resp.Body = interruptedBody{strings.NewReader("partial")}
		return resp, nil
	})
	err := attempt(func() { s.download("/download", filepath.Join(a.Root, "lecture.pdf"), "new") })
	if calls != 1 || !errors.Is(err, io.ErrUnexpectedEOF) || !strings.Contains(err.Error(), "lecture.pdf") || !strings.Contains(err.Error(), "7 / 12") {
		t.Fatalf("missing download evidence or cancellation retried: calls=%d error=%v", calls, err)
	}
}
