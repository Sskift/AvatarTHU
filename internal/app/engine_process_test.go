//go:build !windows

package app

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestEngineTerminationTakesPrecedenceOverEarlierNetworkWarning(t *testing.T) {
	for _, cancelled := range []bool{true, false} {
		t.Run(map[bool]string{true: "cancelled", false: "timeout"}[cancelled], func(t *testing.T) {
			a := testApp(t)
			ctx, cancel := context.WithCancel(a.Ctx)
			defer cancel()
			a.Ctx = ctx
			job := filepath.Join(t.TempDir(), "job")
			mkdir(job)
			exe := filepath.Join(t.TempDir(), "fake-codex")
			writeFile(exe, []byte("#!/bin/sh\necho 'tls handshake eof' >&2\nwhile :; do sleep 1; done\n"), 0700)
			done := make(chan error, 1)
			go func() {
				done <- attempt(func() {
					a.engine("codex", job, "edit text", writerSchema, "局部修改", M{"codex_cli": exe, "stage_timeout": 1})
				})
			}()
			if cancelled {
				deadline := time.Now().Add(5 * time.Second)
				for time.Now().Before(deadline) && tail(executionLog(job, "codex"), 100) == "" {
					time.Sleep(10 * time.Millisecond)
				}
				cancel()
			}
			select {
			case err := <-done:
				if cancelled {
					if !errors.Is(err, context.Canceled) || str(readMap(a.data("codex-execution.json")), "state") != "interrupted" {
						t.Fatal("background stop misreported as network failure", err)
					}
				} else if str(failureFields(err), "category") != "timeout" {
					t.Fatal("deadline misreported as network failure", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("child did not stop")
			}
		})
	}
}
