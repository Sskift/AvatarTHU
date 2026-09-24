package app

import (
	"golang.org/x/sys/windows"
	"os/exec"
	"syscall"
	"unsafe"
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
	return windows.MoveFileEx(a, b, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}
