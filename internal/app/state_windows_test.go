package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestStateSurvivesTemporaryWindowsSharingConflict(t *testing.T) {
	p := filepath.Join(t.TempDir(), "health.json")
	writeJSON(p, M{"state": "starting"})
	path, err := windows.UTF16PtrFromString(p)
	check(err)
	for _, operation := range []string{"read", "replace"} {
		t.Run(operation, func(t *testing.T) {
			handle, err := windows.CreateFile(path, windows.GENERIC_READ, 0, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
			check(err)
			done := make(chan error, 1)
			go func() {
				time.Sleep(100 * time.Millisecond)
				done <- windows.CloseHandle(handle)
			}()
			t.Cleanup(func() {
				if err := <-done; err != nil {
					t.Error(err)
				}
			})
			if operation == "read" {
				if got := str(readMap(p), "state"); got != "starting" {
					t.Fatal(got)
				}
			} else {
				tmp := filepath.Join(filepath.Dir(p), "replacement.json")
				check(os.WriteFile(tmp, []byte(`{"state":"ready"}`), 0600))
				check(replaceFile(tmp, p))
				if got := str(readMap(p), "state"); got != "ready" {
					t.Fatal(got)
				}
			}
		})
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatal(err)
	}
}
