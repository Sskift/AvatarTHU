package app

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

const menubarLabel = "com.local.avatarthu.menubar"

func (a *App) menubarApp() string { return a.desktopApp() }
func (a *App) menubarExecutable() string {
	if exe := desktopExecutable(a.desktopApp()); exists(exe) {
		return exe
	}
	return filepath.Join(a.menubarApp(), "Contents", "MacOS", "AvatarTHUMenuBar")
}
func (a *App) menubarPlist() string {
	home, e := os.UserHomeDir()
	check(e)
	return filepath.Join(home, "Library", "LaunchAgents", menubarLabel+".plist")
}
func menubarDomain() string { return fmt.Sprintf("gui/%d/%s", os.Getuid(), menubarLabel) }

func (a *App) installMenubar(current string) {
	source := desktopSource(current)
	if source == "" || abs(source) == abs(a.menubarApp()) {
		return
	}
	staging := a.menubarApp() + ".new-" + randomID(4)
	defer os.RemoveAll(staging)
	check(copyDesktopBundle(source, staging))
	// A fresh isolated installation must not stop another data directory's UI.
	wasLoaded := exists(a.menubarExecutable()) && a.quiet("launchctl", "print", menubarDomain())
	if wasLoaded {
		a.quiet("launchctl", "bootout", menubarDomain())
		defer a.startMenubarIfEnabled()
	} else if a.desktopRunning() {
		defer a.startMenubarIfEnabled()
	}
	a.quitDesktop()
	check(replaceMenubarBundle(staging, a.menubarApp()))
	if os.Getenv("AVATARTHU_HOME") == "" {
		home, e := os.UserHomeDir()
		check(e)
		applications := filepath.Join(home, "Applications")
		mkdir(applications)
		shortcut := filepath.Join(applications, "AvatarTHU.app")
		if _, e := os.Lstat(shortcut); os.IsNotExist(e) {
			check(os.Symlink(a.menubarApp(), shortcut))
		}
	}
	fmt.Println("已安装 AvatarTHU 桌面 App：" + a.menubarApp())
}

func (a *App) menubarSpec() string {
	return `<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd"><plist version="1.0"><dict><key>Label</key><string>` + menubarLabel + `</string><key>ProgramArguments</key><array><string>` + esc(a.menubarExecutable()) + `</string><string>--hidden</string><string>--home</string><string>` + esc(a.Root) + `</string></array><key>WorkingDirectory</key><string>` + esc(a.Root) + `</string><key>EnvironmentVariables</key><dict><key>AVATARTHU_HOME</key><string>` + esc(a.Root) + `</string><key>PATH</key><string>` + esc(os.Getenv("PATH")) + `</string></dict><key>RunAtLoad</key><true/><key>LimitLoadToSessionType</key><string>Aqua</string><key>StandardOutPath</key><string>` + esc(filepath.Join(a.Root, "logs", "menubar.log")) + `</string><key>StandardErrorPath</key><string>` + esc(filepath.Join(a.Root, "logs", "menubar.log")) + `</string></dict></plist>`
}
func (a *App) setMenubarEnabled(enabled bool) {
	defer a.lock("settings", true)()
	cfg := a.config()
	cfg["menubar_enabled"] = enabled
	a.saveConfig(cfg)
}
func (a *App) startMenubar() {
	ensure(exists(a.menubarExecutable()), "此安装没有菜单栏组件，请从新版 macOS 发布包解压后运行 ./avatarthu init --no-login --no-start（保留同目录 AvatarTHU.app）")
	mkdir(filepath.Join(a.Root, "logs"))
	writeFile(a.menubarPlist(), []byte(a.menubarSpec()), 0600)
	var last error
	for i := 0; i < 5; i++ {
		if a.quiet("launchctl", "print", menubarDomain()) && a.quiet("launchctl", "kickstart", menubarDomain()) {
			return
		}
		last = attempt(func() {
			a.serviceCommand("launchctl", "bootstrap", fmt.Sprintf("gui/%d", os.Getuid()), a.menubarPlist())
		})
		if last == nil {
			return
		}
		// bootout may still be retiring the previous GUI process during upgrade.
		time.Sleep(200 * time.Millisecond)
	}
	check(last)
}
func (a *App) startMenubarIfEnabled() {
	cfg := a.config()
	if enabled, ok := cfg["menubar_enabled"].(bool); ok && !enabled {
		return
	}
	if !exists(a.menubarExecutable()) {
		return
	}
	if e := attempt(a.startMenubar); e != nil {
		fmt.Println("后台已启动；菜单栏未启动：" + safeError(e) + "。可运行 avatarthu menubar start 重试。")
	}
}
func (a *App) stopMenubar() {
	a.quiet("launchctl", "bootout", menubarDomain())
	_ = os.Remove(a.menubarPlist())
	a.quitDesktop()
}
func (a *App) menubarCommand(action string) {
	switch action {
	case "start":
		a.startMenubar()
		a.setMenubarEnabled(true)
		fmt.Println("菜单栏已启动，登录系统时自动显示；点击菜单栏直接打开 App；课程后台独立运行。")
	case "stop":
		a.setMenubarEnabled(false)
		a.stopMenubar()
		fmt.Println("菜单栏已关闭，已关闭菜单栏自启动；后台 loop 不受影响。")
	case "status":
		m := readMap(a.data("menubar-health.json"))
		pid := number(m, "pid", 0)
		running := str(m, "state") == "running" && pid > 1 && syscall.Kill(pid, 0) == nil
		fmt.Printf("macOS 菜单栏：已安装=%t，运行中=%t，PID=%d\n", exists(a.menubarExecutable()), running, pid)
	default:
		panic(fmt.Errorf("用法：avatarthu menubar start|stop|status"))
	}
}
