//go:build !darwin

package app

import "fmt"

func (a *App) installMenubar(current string) {}
func (a *App) startMenubarIfEnabled()        {}
func (a *App) stopMenubar()                  {}
func (a *App) menubarCommand(action string) {
	panic(fmt.Errorf("菜单栏监控仅适用于 macOS；此系统请使用 avatarthu status 或 avatarthu service status"))
}
