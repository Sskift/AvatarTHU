package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func (a *App) desktopApp() string {
	name := "AvatarTHU"
	if runtime.GOOS == "darwin" {
		name += ".app"
	}
	return filepath.Join(a.Root, "apps", name)
}

func desktopExecutable(bundle string) string {
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(bundle, "Contents", "MacOS", "AvatarTHU")
	case "windows":
		return filepath.Join(bundle, "AvatarTHU.exe")
	default:
		return filepath.Join(bundle, "AvatarTHU")
	}
}

func (a *App) launchDesktop(args ...string) {
	exe := desktopExecutable(a.desktopApp())
	ensure(exists(exe), "尚未安装桌面 App。请解压完整发布包后运行 ./avatarthu init --no-login --no-start，保留同目录 AvatarTHU App。")
	args = append([]string{"--home", a.Root}, args...)
	var cmd *exec.Cmd
	if runtime.GOOS == "darwin" {
		cmd = exec.Command("/usr/bin/open", append([]string{"-n", "-a", a.desktopApp(), "--args"}, args...)...)
		check(cmd.Run())
	} else {
		cmd = exec.Command(exe, args...)
		check(cmd.Start())
		check(cmd.Process.Release())
	}
}

func (a *App) desktopRunning() bool {
	h := readMap(a.data("menubar-health.json"))
	return str(h, "state") == "running" && workspaceFresh(str(h, "time"), time.Now(), 45*time.Second)
}

func (a *App) quitDesktop() {
	if !exists(desktopExecutable(a.desktopApp())) || !a.desktopRunning() {
		return
	}
	a.launchDesktop("--quit")
	deadline := time.Now().Add(15 * time.Second)
	for a.desktopRunning() {
		check(a.Ctx.Err())
		ensure(time.Now().Before(deadline), "App 仍在退出，请稍后重试；原有安装保持不变")
		time.Sleep(100 * time.Millisecond)
	}
	// The receipt is written just before Electron releases its executable files.
	time.Sleep(300 * time.Millisecond)
}

func (a *App) openDesktop(tid string, editor bool, view string) {
	ensure(view == "overview" || view == "courses" || view == "files" || view == "artifacts" || view == "settings", "无效的工作台页面")
	args := []string{"--view", view}
	if tid != "" {
		ensure(len(readMap(a.taskPath(tid))) > 0, "找不到这份作业")
		args = append(args, "--task", tid)
	}
	if editor {
		args = append(args, "--editor")
	}
	a.launchDesktop(args...)
	fmt.Println("已打开 AvatarTHU App。")
}

func desktopSource(current string) string {
	name := "AvatarTHU"
	if runtime.GOOS == "darwin" {
		name += ".app"
	}
	source := filepath.Join(filepath.Dir(current), name)
	if exists(desktopExecutable(source)) {
		return source
	}
	// The app also ships the Go executable for first launch without a CLI install.
	for dir, n := filepath.Dir(current), 0; n < 5; dir, n = filepath.Dir(dir), n+1 {
		if exists(desktopExecutable(dir)) && filepath.Base(dir) == name {
			return dir
		}
	}
	return ""
}

func copyDesktopBundle(source, target string) error {
	source, err := filepath.EvalSymlinks(source)
	if err != nil {
		return err
	}
	return filepath.WalkDir(source, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		rel, e := filepath.Rel(source, p)
		if e != nil {
			return e
		}
		dest := filepath.Join(target, rel)
		info, e := os.Lstat(p)
		if e != nil {
			return e
		}
		if d.Type()&os.ModeSymlink != 0 {
			resolved, e := filepath.EvalSymlinks(p)
			if e != nil {
				return e
			}
			within, e := filepath.Rel(source, resolved)
			if e != nil || within == ".." || strings.HasPrefix(within, ".."+string(filepath.Separator)) {
				return fmt.Errorf("桌面 App 包含指向包外的符号链接")
			}
			link, e := os.Readlink(p)
			if e != nil || filepath.IsAbs(link) {
				return fmt.Errorf("桌面 App 包含无效符号链接")
			}
			if e := os.MkdirAll(filepath.Dir(dest), 0755); e != nil {
				return e
			}
			return os.Symlink(link, dest)
		}
		if d.IsDir() {
			return os.MkdirAll(dest, info.Mode().Perm())
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("桌面 App 包含非普通文件")
		}
		copyFile(p, dest)
		return os.Chmod(dest, info.Mode().Perm())
	})
}

func replaceMenubarBundle(staging, target string) error {
	backup := target + ".old-" + randomID(4)
	_, e := os.Lstat(target)
	hadPrevious := e == nil
	if e != nil && !os.IsNotExist(e) {
		return e
	}
	if hadPrevious {
		if e := os.Rename(target, backup); e != nil {
			return e
		}
	}
	if e := os.Rename(staging, target); e != nil {
		if hadPrevious {
			if rollback := os.Rename(backup, target); rollback != nil {
				return fmt.Errorf("App 更新失败：%v；旧版保留在 %s：%v", e, backup, rollback)
			}
		}
		return e
	}
	if hadPrevious {
		_ = os.RemoveAll(backup)
	}
	return nil
}
