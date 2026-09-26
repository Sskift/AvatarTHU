package app

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func setupProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
}
func bindProcess(cmd *exec.Cmd) func() {
	job, e := windows.CreateJobObject(nil, nil)
	check(e)
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	_, e = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits)))
	if e != nil {
		windows.CloseHandle(job)
		check(e)
	}
	h, e := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if e != nil {
		windows.CloseHandle(job)
		check(e)
	}
	e = windows.AssignProcessToJobObject(job, h)
	windows.CloseHandle(h)
	if e != nil {
		windows.CloseHandle(job)
		check(e)
	}
	return func() { _ = windows.TerminateJobObject(job, 1); _ = windows.CloseHandle(job) }
}
func replaceFile(src, dst string) error {
	a, e := windows.UTF16PtrFromString(src)
	if e != nil {
		return e
	}
	b, e := windows.UTF16PtrFromString(dst)
	if e != nil {
		return e
	}
	return retrySharedFile(func() error {
		return windows.MoveFileEx(a, b, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
	})
}

// Windows briefly denies opens or replacement while another process has a
// conflicting handle. Retry only sharing/lock conflicts, not permission errors.
func retrySharedFile(op func() error) error {
	deadline := time.Now().Add(time.Second)
	for {
		err := op()
		if (!errors.Is(err, windows.ERROR_SHARING_VIOLATION) && !errors.Is(err, windows.ERROR_LOCK_VIOLATION)) || !time.Now().Before(deadline) {
			return err
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func readStateFile(p string) (b []byte, err error) {
	err = retrySharedFile(func() error {
		var e error
		b, e = os.ReadFile(p)
		return e
	})
	return
}
