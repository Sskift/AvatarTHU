package app

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCourseSyncRunsWhileModelWorkerIsBusy(t *testing.T) {
	a := testApp(t)
	ctx, cancel := context.WithCancel(a.Ctx)
	a.Ctx = ctx
	a.syncWake = make(chan struct{}, 1)
	writeJSON(a.data("schedule.json"), M{"last_sync_at": stamp()})
	defer a.lock("daily", false)()
	defer a.lock("model-worker", false)()
	done := make(chan struct{})
	go func() { defer close(done); a.syncLoop(ctx) }()
	defer func() { cancel(); <-done }()
	request := editorResult(t, editorCall(t, a, "/api/workspace/scan", M{}))
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		scan := readMap(a.data("workspace-scan.json"))
		if str(scan, "state") == "error" {
			if str(scan, "id") != str(request, "id") || !strings.Contains(str(scan, "error"), "尚未登录") || str(scan, "started_at") == "" {
				t.Fatal(scan)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("course sync waited for the writer queue")
}

func TestCourseSyncDoesNotWaitForBusyAssignment(t *testing.T) {
	a := testApp(t)
	version := "old"
	s := fakeSchool(a, func(r *http.Request) (*http.Response, error) {
		var body string
		switch {
		case strings.Contains(r.URL.Path, "loadCourseBySemesterId"):
			body = `{"resultList":[{"wlkcid":"course1","kcm":"Course","xnxq":"2026"}]}`
		case strings.Contains(r.URL.Path, "kjxxbByWlkcidAndSizeForStudent"):
			body = `{"result":"success","object":[]}`
		case strings.Contains(r.URL.Path, "zyListWj"):
			rows := []M{{"xszyid": "one", "zyid": "one", "bt": "Homework", "jzsjStr": "2099-01-01 12:00:00"}}
			if version == "new" {
				rows = append(rows, M{"xszyid": "two", "zyid": "two", "bt": "New homework", "jzsjStr": "2099-01-01 12:00:00"})
			}
			body = string(jsonBytes(M{"result": "success", "object": M{"aaData": rows}}))
		case strings.Contains(r.URL.Path, "viewTj"):
			body = `<div class="boxbox">` + version + ` instructions</div>`
		default:
			panic("unexpected school request: " + r.URL.Path)
		}
		return response(r, body, "application/json", int64(len(body))), nil
	})
	s.Semester = "2026"
	a.syncSchoolAssignments(s, 0)
	st := a.tasks()[0]
	tid := str(st, "task_id")
	merge(st, M{"status": "working", "revision": 3, "card_message_id": "keep-card", "nonce": "keep-nonce"})
	a.saveTask(st)
	job := filepath.Join(a.Root, "running-job")
	a.prepare(st, job)
	input := digest(filepath.Join(job, "input", "assignment.md"))
	before := digest(a.taskPath(tid))
	unlock := a.lock(tid, false)
	defer func() {
		if unlock != nil {
			unlock()
		}
	}()
	version = "new"
	done := make(chan error, 1)
	go func() { done <- attempt(func() { a.syncSchoolAssignments(s, 0) }) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		unlock()
		unlock = nil
		<-done
		t.Fatal("a busy homework blocked course sync")
	}
	if digest(a.taskPath(tid)) != before || digest(filepath.Join(job, "input", "assignment.md")) != input {
		t.Fatal("sync overwrote a running task or its input snapshot")
	}
	if len(a.tasks()) != 2 || !exists(a.pendingSyncPath(tid)) {
		t.Fatal("new homework missing or busy update was lost")
	}
	expectError(t, "课程材料已更新", func() { a.prepare(st, filepath.Join(a.Root, "next-attempt")) })
	unlock()
	unlock = nil
	// A restart must apply the saved update without another network request.
	restarted := New(context.Background(), a.Root)
	writeJSON(a.data("schedule.json"), M{"last_sync_at": stamp()})
	if !restarted.syncScheduled(true) {
		t.Fatal("cached update failed")
	}
	got := readMap(a.taskPath(tid))
	if str(got, "status") != "revision_ready" || !strings.Contains(str(got, "description"), "new instructions") || exists(a.pendingSyncPath(tid)) {
		t.Fatal("busy update not recovered", got)
	}
	if str(got, "card_message_id") != "keep-card" || str(got, "nonce") != "keep-nonce" || number(got, "revision", 0) != 3 || str(got, "source_hash") != materialsHash(got) {
		t.Fatal("update changed approval identity or material fingerprint", got)
	}
}

func TestDeferredSyncRespectsLatestProtectedTaskState(t *testing.T) {
	for _, status := range []string{"submitted", "submitting", "submission_unknown", "ignored"} {
		for _, closed := range []bool{false, true} {
			t.Run(status+"/"+map[bool]string{false: "changed", true: "closed"}[closed], func(t *testing.T) {
				a := testApp(t)
				st := frozen(t, a)
				tid := str(st, "task_id")
				writeJSON(a.pendingSyncPath(tid), M{"closed": closed, "meta": M{"description": "updated"}, "source_hash": "new"})
				st["status"] = status
				a.saveTask(st)
				writeJSON(a.data("schedule.json"), M{"last_sync_at": stamp()})
				a.syncScheduled(true)
				got := readMap(a.taskPath(tid))
				if str(got, "status") != status || str(got, "description") != str(st, "description") || str(got, "card_message_id") != str(st, "card_message_id") || digest(str(got, "submission")) != str(st, "sha256") {
					t.Fatal("deferred sync damaged the latest protected task", got)
				}
			})
		}
	}
}

func TestCourseSyncCoalescesRequests(t *testing.T) {
	a := testApp(t)
	defer a.lock("course-sync", false)()
	writeJSON(a.data("workspace-scan.json"), M{"id": "current", "state": "running"})
	for i := 0; i < 2; i++ {
		result := editorResult(t, editorCall(t, a, "/api/workspace/scan", M{}))
		if str(result, "id") != "current" || !a.run(false, true, "") {
			t.Fatal("duplicate sync started")
		}
	}
	if str(readMap(a.data("workspace-scan.json")), "state") != "running" {
		t.Fatal("concurrent caller overwrote active sync status")
	}
}

func TestDeferredRemoteClosureKeepsCompletedArtifacts(t *testing.T) {
	a := testApp(t)
	st := frozen(t, a)
	tid := str(st, "task_id")
	st["status"] = "working"
	a.saveTask(st)
	writeJSON(a.data("schedule.json"), M{"last_sync_at": stamp()})
	writeJSON(a.pendingSyncPath(tid), M{"closed": true})
	func() {
		defer a.lock(tid, false)()
		a.syncScheduled(true)
		if str(readMap(a.taskPath(tid)), "status") != "working" || !exists(a.pendingSyncPath(tid)) {
			t.Fatal("remote closure overwrote a running task")
		}
		st["status"] = "awaiting"
		a.saveTask(st)
	}()
	a.syncScheduled(true)
	got := readMap(a.taskPath(tid))
	if str(got, "status") != "closed_remote" || str(got, "card_message_id") != str(st, "card_message_id") || digest(str(got, "submission")) != str(st, "sha256") || exists(a.pendingSyncPath(tid)) {
		t.Fatal("remote closure lost its update or completed artifacts", got)
	}
}
