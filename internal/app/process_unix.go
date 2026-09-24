//go:build !windows

package app

import (
	"os"
	"os/exec"
	"syscall"
)

func setupProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
func bindProcess(cmd *exec.Cmd) func() {
	return func() {
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
	}
}
func replaceFile(src, dst string) error { return os.Rename(src, dst) }
