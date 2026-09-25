package app

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode/utf16"
)

const serviceLabel = "com.local.avatarthu.daemon"

func (a *App) installedBinary() string {
	name := "avatarthu"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(a.Root, "bin", name)
}
func (a *App) init(noLogin, noStart bool) {
	mkdir(a.Root)
	mkdir(filepath.Join(a.Root, "logs"))
	if runtime.GOOS != "windows" {
		legacy := a.data("courses")
		entry := filepath.Join(a.Root, "courses")
		if st, e := os.Stat(legacy); e == nil && st.IsDir() {
			if _, e := os.Lstat(entry); os.IsNotExist(e) {
				check(os.Symlink(legacy, entry))
			}
		}
	}
	defer a.lock("install", true)()
	cfg := a.config()
	defaults := M{"session": filepath.Join(a.Root, "session.json"), "poll_interval_seconds": 43200, "review_mode": "claude-codex", "max_review_rounds": 3, "stage_timeout": 7200}
	if len(cfg) == 0 {
		defaults["lark_enabled"] = false
	}
	for k, v := range defaults {
		if cfg[k] == nil {
			cfg[k] = v
		}
	}
	for _, tool := range []string{"claude", "codex", "lark-cli"} {
		key := tool + "_cli"
		if tool == "lark-cli" {
			key = "lark_cli"
		}
		if p := findExecutable(tool, str(cfg, key)); p != "" {
			cfg[key] = p
		}
	}
	a.saveConfig(cfg)
	current, e := os.Executable()
	check(e)
	current, e = filepath.EvalSymlinks(current)
	check(e)
	target := a.installedBinary()
	if abs(current) != abs(target) {
		if runtime.GOOS == "windows" && exists(target) {
			check(os.Rename(target, target+".old-"+randomID(3)))
		}
		copyFile(current, target)
		check(os.Chmod(target, 0755))
	}
	a.installMenubar(current)
	home, e := os.UserHomeDir()
	check(e)
	alias := filepath.Join(home, ".avatarthu")
	if runtime.GOOS != "windows" && os.Getenv("AVATARTHU_HOME") == "" {
		if _, e := os.Lstat(alias); os.IsNotExist(e) {
			check(os.Symlink(a.Root, alias))
		} else if real, e := filepath.EvalSymlinks(alias); e == nil && abs(real) != abs(a.Root) {
			fmt.Println("提示：~/.avatarthu 已指向另一目录，保持原入口：" + real)
		}
	}
	bin := filepath.Join(home, ".local", "bin")
	if os.Getenv("AVATARTHU_HOME") != "" {
		bin = filepath.Join(a.Root, "bin")
	}
	mkdir(bin)
	entry := filepath.Join(bin, filepath.Base(target))
	if abs(entry) != abs(target) {
		if runtime.GOOS == "windows" {
			if exists(entry) {
				_ = os.Rename(entry, entry+".old-"+randomID(3))
			}
			copyFile(target, entry)
		} else {
			if info, e := os.Lstat(entry); e == nil {
				ensure(info.Mode()&os.ModeSymlink != 0 || info.Mode().IsRegular(), "命令入口不是普通文件")
				backup := filepath.Join(a.Root, "legacy-launchers", filepath.Base(entry)+"-"+randomID(3))
				mkdir(filepath.Dir(backup))
				check(os.Rename(entry, backup))
			}
			check(os.Symlink(target, entry))
		}
	}
	if os.Getenv("AVATARTHU_HOME") == "" {
		installUserPath(bin)
	}
	fmt.Println("已安装原生程序：" + entry + "\n课程和状态：" + a.Root + "\n不需要 Python、pip、虚拟环境或 Go 编译器。")
	if !noLogin {
		state := a.keepalive(true, false)
		if str(state, "state") != "valid" {
			a.loginTHU(false)
		}
	}
	if !noStart {
		a.serviceStart()
	}
}
func (a *App) serviceSpec() string {
	exe := a.installedBinary()
	return `<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd"><plist version="1.0"><dict><key>Label</key><string>` + serviceLabel + `</string><key>ProgramArguments</key><array><string>` + esc(exe) + `</string><string>daemon</string></array><key>WorkingDirectory</key><string>` + esc(a.Root) + `</string><key>EnvironmentVariables</key><dict><key>AVATARTHU_HOME</key><string>` + esc(a.Root) + `</string><key>PATH</key><string>` + esc(os.Getenv("PATH")) + `</string></dict><key>StandardOutPath</key><string>` + esc(filepath.Join(a.Root, "logs", "daemon.log")) + `</string><key>StandardErrorPath</key><string>` + esc(filepath.Join(a.Root, "logs", "daemon.log")) + `</string><key>RunAtLoad</key><true/><key>KeepAlive</key><true/><key>ThrottleInterval</key><integer>60</integer></dict></plist>`
}
func (a *App) windowsSpec(owner string) string {
	start := time.Now().Add(time.Minute).Format("2006-01-02T15:04:05")
	return `<?xml version="1.0" encoding="UTF-16"?><Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task"><RegistrationInfo><Description>AvatarTHU course polling and session keepalive</Description></RegistrationInfo><Triggers><LogonTrigger><Enabled>true</Enabled><UserId>` + esc(owner) + `</UserId></LogonTrigger><TimeTrigger><Repetition><Interval>PT1M</Interval><StopAtDurationEnd>false</StopAtDurationEnd></Repetition><StartBoundary>` + start + `</StartBoundary><Enabled>true</Enabled></TimeTrigger></Triggers><Principals><Principal id="Author"><UserId>` + esc(owner) + `</UserId><LogonType>InteractiveToken</LogonType><RunLevel>LeastPrivilege</RunLevel></Principal></Principals><Settings><MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy><DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries><StopIfGoingOnBatteries>false</StopIfGoingOnBatteries><StartWhenAvailable>true</StartWhenAvailable><AllowStartOnDemand>true</AllowStartOnDemand><Enabled>true</Enabled><ExecutionTimeLimit>PT0S</ExecutionTimeLimit><RestartOnFailure><Interval>PT1M</Interval><Count>999</Count></RestartOnFailure></Settings><Actions Context="Author"><Exec><Command>` + esc(a.installedBinary()) + `</Command><Arguments>daemon --home &quot;` + esc(a.Root) + `&quot;</Arguments><WorkingDirectory>` + esc(a.Root) + `</WorkingDirectory></Exec></Actions></Task>`
}
func utf16Bytes(s string) []byte {
	r := utf16.Encode([]rune(s))
	b := []byte{0xff, 0xfe}
	for _, v := range r {
		b = append(b, byte(v), byte(v>>8))
	}
	return b
}
func (a *App) quiet(argv ...string) bool {
	_, _, e := capture(a.Ctx, a.Root, 30*time.Second, argv...)
	return e == nil
}
func (a *App) serviceCommand(argv ...string) {
	out, se, e := capture(a.Ctx, a.Root, 30*time.Second, argv...)
	if e != nil {
		panic(fmt.Errorf("后台服务配置失败：%s %s", safeError(string(out)+string(se)), safeError(e)))
	}
}
func (a *App) serviceLoaded() bool {
	switch runtime.GOOS {
	case "darwin":
		return a.quiet("launchctl", "print", fmt.Sprintf("gui/%d/%s", os.Getuid(), serviceLabel))
	case "windows":
		return a.quiet("schtasks", "/Query", "/TN", `\AvatarTHU\daemon`)
	case "linux":
		return a.quiet("systemctl", "--user", "is-active", "avatarthu.service")
	}
	return false
}
func (a *App) retireLegacy() {
	switch runtime.GOOS {
	case "darwin":
		home, e := os.UserHomeDir()
		check(e)
		labels := []string{}
		for _, prefix := range []string{"com.local.avatarthu.", "com.local.thu-learn-loop."} {
			for _, name := range []string{"daily", "actions", "messages", "keepalive"} {
				labels = append(labels, prefix+name)
			}
		}
		if a.config()["migrated_autothu_session"] != nil {
			labels = append(labels, "com.local.autothu.keepalive")
		}
		for _, label := range labels {
			p := filepath.Join(home, "Library/LaunchAgents", label+".plist")
			if !exists(p) {
				continue
			}
			a.quiet("launchctl", "bootout", fmt.Sprintf("gui/%d/%s", os.Getuid(), label))
			backup := filepath.Join(a.Root, "legacy-launchagents", filepath.Base(p))
			if !exists(backup) {
				copyFile(p, backup)
			}
			check(os.Remove(p))
		}
	case "windows":
		for _, name := range []string{"daily", "actions", "messages", "keepalive"} {
			p := `\AvatarTHU\` + name
			if a.quiet("schtasks", "/Query", "/TN", p) {
				a.quiet("schtasks", "/End", "/TN", p)
				a.serviceCommand("schtasks", "/Delete", "/TN", p, "/F")
			}
		}
	}
}
func (a *App) serviceStart() {
	ensure(exists(a.installedBinary()), "请先运行 avatarthu init 安装命令")
	a.retireLegacy()
	if a.serviceLoaded() {
		a.startMenubarIfEnabled()
		fmt.Println("后台 loop 已在运行，通知设置在 15 秒内生效。")
		return
	}
	mkdir(filepath.Join(a.Root, "logs"))
	home, e := os.UserHomeDir()
	check(e)
	switch runtime.GOOS {
	case "darwin":
		p := filepath.Join(home, "Library/LaunchAgents", serviceLabel+".plist")
		writeFile(p, []byte(a.serviceSpec()), 0600)
		a.serviceCommand("launchctl", "bootstrap", fmt.Sprintf("gui/%d", os.Getuid()), p)
	case "windows":
		owner, e := user.Current()
		check(e)
		p := filepath.Join(a.Root, "services", "daemon.xml")
		writeFile(p, utf16Bytes(a.windowsSpec(owner.Username)), 0600)
		a.serviceCommand("schtasks", "/Create", "/TN", `\AvatarTHU\daemon`, "/XML", p, "/F")
		a.serviceCommand("schtasks", "/Run", "/TN", `\AvatarTHU\daemon`)
	case "linux":
		p := filepath.Join(home, ".config/systemd/user/avatarthu.service")
		unit := "[Unit]\nDescription=AvatarTHU\n[Service]\nExecStart=" + systemdQuote(a.installedBinary()) + " daemon --home " + systemdQuote(a.Root) + "\nRestart=always\nRestartSec=60\nKillMode=control-group\n[Install]\nWantedBy=default.target\n"
		writeFile(p, []byte(unit), 0600)
		a.serviceCommand("systemctl", "--user", "daemon-reload")
		a.serviceCommand("systemctl", "--user", "enable", "--now", "avatarthu.service")
	default:
		panic(fmt.Errorf("此系统请使用 avatarthu daemon 在前台运行"))
	}
	a.startMenubarIfEnabled()
	fmt.Println("后台 loop 已启动。10 分钟保活，课程扫描按配置执行，退出终端后继续运行；登录系统自动启动，休眠唤醒后补查。")
}
func systemdQuote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "%", "%%").Replace(s) + `"`
}
func (a *App) serviceStop() {
	home, e := os.UserHomeDir()
	check(e)
	switch runtime.GOOS {
	case "darwin":
		a.quiet("launchctl", "bootout", fmt.Sprintf("gui/%d/%s", os.Getuid(), serviceLabel))
		_ = os.Remove(filepath.Join(home, "Library/LaunchAgents", serviceLabel+".plist"))
	case "windows":
		a.quiet("schtasks", "/End", "/TN", `\AvatarTHU\daemon`)
		a.quiet("schtasks", "/Delete", "/TN", `\AvatarTHU\daemon`, "/F")
	case "linux":
		a.quiet("systemctl", "--user", "disable", "--now", "avatarthu.service")
		_ = os.Remove(filepath.Join(home, ".config/systemd/user/avatarthu.service"))
		a.quiet("systemctl", "--user", "daemon-reload")
	}
	a.retireLegacy()
	fmt.Println("后台服务已停用；课程、会话和产物均保留。")
}
