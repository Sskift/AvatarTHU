//go:build !darwin

package app

import (
	"fmt"
	"os"
	"time"
)

func (a *App) installMenubar(current string) {
	source := desktopSource(current)
	if source == "" || abs(source) == abs(a.desktopApp()) {
		return
	}
	staging := a.desktopApp() + ".new-" + randomID(4)
	defer os.RemoveAll(staging)
	check(copyDesktopBundle(source, staging))
	if a.desktopRunning() {
		a.quitDesktop()
		defer a.startMenubarIfEnabled()
	}
	check(replaceMenubarBundle(staging, a.desktopApp()))
	fmt.Println("已安装 AvatarTHU 桌面 App：" + a.desktopApp())
}
func (a *App) startMenubarIfEnabled() {
	if enabled, ok := a.config()["menubar_enabled"].(bool); ok && !enabled {
		return
	}
	if exists(desktopExecutable(a.desktopApp())) {
		if e := attempt(func() { a.launchDesktop("--hidden") }); e != nil {
			fmt.Println(safeError(e))
		}
	}
}
func (a *App) stopMenubar() {
	if exists(desktopExecutable(a.desktopApp())) {
		a.launchDesktop("--quit")
	}
}
func (a *App) menubarCommand(action string) {
	if action == "status" {
		h := readMap(a.data("menubar-health.json"))
		fmt.Printf("桌面 App：已安装=%t，运行中=%t\n", exists(desktopExecutable(a.desktopApp())), str(h, "state") == "running" && workspaceFresh(str(h, "time"), time.Now(), 45*time.Second))
		return
	}
	ensure(action == "start" || action == "stop", "用法：avatarthu menubar start|stop|status")
	func() {
		defer a.lock("settings", true)()
		cfg := a.config()
		cfg["menubar_enabled"] = action == "start"
		a.saveConfig(cfg)
	}()
	if action == "start" {
		a.launchDesktop("--hidden")
	} else {
		a.stopMenubar()
	}
}
