package app

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

func command(ctx context.Context, argv ...string) *exec.Cmd {
	ensure(len(argv) > 0 && argv[0] != "", "未找到可执行文件")
	path := argv[0]
	if runtime.GOOS == "windows" {
		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".cmd" || ext == ".bat" {
			b, e := os.ReadFile(path)
			check(e)
			matches := regexp.MustCompile(`(?i)%~?dp0%?[\\/]([^\r\n"]+?\.(?:js|cjs|mjs))`).FindAllStringSubmatch(string(b), -1)
			entry := ""
			for _, m := range matches {
				candidate := filepath.Join(filepath.Dir(path), filepath.FromSlash(strings.ReplaceAll(m[1], `\`, "/")))
				if exists(candidate) {
					entry = candidate
					break
				}
			}
			ensure(entry != "", "无法识别 CLI 的 Windows 启动脚本；请安装原生程序或官方 npm 版本："+path)
			node := filepath.Join(filepath.Dir(path), "node.exe")
			if !exists(node) {
				node = findExecutable("node", "")
			}
			ensure(node != "", "此 CLI 的 npm 安装需要 Node.js：请安装 Node.js LTS 或改用该 CLI 的原生安装版")
			argv = append([]string{node, entry}, argv[1:]...)
		}
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	setupProcess(cmd)
	cmd.WaitDelay = 10 * time.Second
	return cmd
}
func capture(ctx context.Context, dir string, timeout time.Duration, argv ...string) ([]byte, []byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := command(ctx, argv...)
	cmd.Dir = dir
	var out, err bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &err
	e := cmd.Start()
	if e != nil {
		return nil, nil, e
	}
	cleanup := bindProcess(cmd)
	defer cleanup()
	e = cmd.Wait()
	if ctx.Err() != nil {
		e = ctx.Err()
	}
	return out.Bytes(), err.Bytes(), e
}
func interactive(ctx context.Context, dir string, argv ...string) {
	cmd := command(ctx, argv...)
	cmd.Dir = dir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	check(cmd.Start())
	defer bindProcess(cmd)()
	check(cmd.Wait())
}
func findExecutable(name, configured string) string {
	candidates := []string{configured, name}
	home, _ := os.UserHomeDir()
	if runtime.GOOS == "windows" {
		for _, base := range []string{filepath.Join(home, ".local", "bin"), filepath.Join(os.Getenv("APPDATA"), "npm"), filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", name)} {
			candidates = append(candidates, filepath.Join(base, name+".exe"), filepath.Join(base, name+".cmd"))
		}
	} else {
		for _, base := range []string{filepath.Join(home, ".local", "bin"), "/opt/homebrew/bin", "/usr/local/bin"} {
			candidates = append(candidates, filepath.Join(base, name))
		}
	}
	for _, p := range candidates {
		if p == "" {
			continue
		}
		if v, e := exec.LookPath(p); e == nil {
			if name == "lark-cli" {
				return nativeLark(v)
			}
			return v
		}
	}
	return ""
}

// Official npm installations already contain a native Lark executable. Reuse
// it so an idle event subscription doesn't keep an extra Node launcher alive.
func nativeLark(launcher string) string {
	real, err := filepath.EvalSymlinks(launcher)
	if err != nil {
		return launcher
	}
	dirs := []string{filepath.Dir(filepath.Dir(real)), filepath.Join(filepath.Dir(real), "node_modules", "@larksuite", "cli")}
	for _, dir := range dirs {
		b, e := os.ReadFile(filepath.Join(dir, "package.json"))
		if e != nil {
			continue
		}
		var pkg M
		if json.Unmarshal(b, &pkg) != nil || str(pkg, "name") != "@larksuite/cli" {
			continue
		}
		name := "lark-cli"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		native := filepath.Join(dir, "bin", name)
		if exists(native) && !exists(native+".old") {
			if p, e := exec.LookPath(native); e == nil {
				return p
			}
		}
	}
	return launcher
}
func diagnosticError(tool, text, log string) error {
	low := strings.ToLower(text)
	if strings.Contains(low, "fork/exec") && strings.Contains(low, "no such file or directory") {
		low += " executable file not found"
	}
	reason := "CLI 执行失败"
	action := "先在终端单独运行 " + tool + " 排查"
	category, retryable := "execution", true
	patterns := []struct {
		keys           []string
		reason, action string
		category       string
		retryable      bool
	}{
		{[]string{"未找到 cli", "executable file not found"}, "未找到 CLI", "运行 avatarthu tools 查看安装方式，安装后运行 avatarthu configure", "missing", false},
		{[]string{"401", "unauthorized", "authentication", "not logged", "login required", "invalid api key", "尚未登录", "授权失效"}, "未登录或授权失效", "运行 " + tool + " 完成登录", "auth", false},
		{[]string{"429", "402", "rate limit", "usage limit", "quota", "credit", "额度", "余额"}, "额度或频率限制", "检查账号额度或等待限制重置，再手动恢复作业", "quota", false},
		{[]string{"unknown option", "unknown feature", "unrecognized argument", "unexpected argument", "no such command", "版本不兼容"}, "CLI 版本不兼容", "更新 CLI 后运行 avatarthu doctor", "version", false},
		{[]string{"403", "forbidden", "permission denied", "access denied"}, "权限不足", "检查 CLI 账号、可执行文件和工作目录权限", "permission", false},
		{[]string{"connection", "timeout", "timed out", "deadline exceeded", "resolve", "proxy", "certificate", "tls", "502", "503", "504", "network"}, "网络或服务暂时不可用", "检查网络、代理及服务状态；后台 15 分钟后重试，也可手动恢复", "network", true},
		{[]string{"sandbox", "bwrap"}, "CLI 执行环境未就绪", "在终端单独运行 CLI，完成它要求的首次设置", "environment", false},
	}
	found := false
	for _, p := range patterns {
		for _, k := range p.keys {
			if strings.Contains(low, k) {
				reason = p.reason
				action = p.action
				category, retryable = p.category, p.retryable
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found && strings.Contains(low, "model") {
		reason = "CLI 默认配置不可用"
		action = "先单独运行 CLI 排查自己的默认设置；AvatarTHU 不更改模型"
		category, retryable = "configuration", false
	}
	return &toolFailure{Tool: tool, Category: category, Reason: reason, Action: action, Log: log, Retryable: retryable}
}
func (a *App) probe(tool, path string) M {
	r := M{"tool": tool, "checked_at": stamp(), "usable": false}
	path = findExecutable(tool, path)
	if path == "" {
		merge(r, failureFields(diagnosticError(tool, "未找到 CLI", "")))
		return r
	}
	err := attempt(func() {
		out, se, e := capture(a.Ctx, a.Root, 20*time.Second, path, "--version")
		if e != nil {
			panic(diagnosticError(tool, string(out)+string(se)+e.Error(), ""))
		}
		r["version"] = strings.TrimSpace(string(out))
		args := []string{path, "login", "status"}
		if tool == "claude" {
			args = []string{path, "auth", "status", "--json"}
		}
		out, se, e = capture(a.Ctx, a.Root, 30*time.Second, args...)
		if e != nil {
			panic(diagnosticError(tool, string(out)+string(se)+e.Error(), ""))
		}
		if tool == "claude" {
			m := parseMap(out)
			ensure(boolean(m, "loggedIn"), "claude 尚未登录；请运行 claude 完成登录")
		}
		ensure(!strings.Contains(strings.ToLower(string(out)+string(se)), "not logged"), tool+" 尚未登录")
		args = []string{path, "--help"}
		required := []string{"--safe-mode", "--no-session-persistence", "--json-schema"}
		if tool == "codex" {
			args = []string{path, "exec", "--help"}
			required = []string{"--ephemeral", "--ignore-rules", "--output-schema"}
		}
		out, _, e = capture(a.Ctx, a.Root, 20*time.Second, args...)
		check(e)
		for _, flag := range required {
			ensure(strings.Contains(string(out), flag), tool+" 版本不兼容，缺少 "+flag+"；请更新 CLI")
		}
		merge(r, M{"usable": true, "reason": "可启动，登录检查通过", "action": "实际执行中的额度、网络和权限错误会保存在作业状态中"})
	})
	if err != nil {
		failure := failureFields(err)
		if len(failure) == 0 {
			failure = failureFields(diagnosticError(tool, err.Error(), ""))
		}
		merge(r, failure)
	}
	return r
}
func (a *App) inspectPair(plan M) []M {
	r := []M{a.probe("claude", str(plan, "claude_cli")), a.probe("codex", str(plan, "codex_cli"))}
	writeJSON(a.data("cli-health.json"), r)
	return r
}
func (a *App) requirePair(plan M) {
	if a.RunModel != nil {
		return
	}
	var first error
	for _, r := range a.inspectPair(plan) {
		if !boolean(r, "usable") {
			failure := &toolFailure{Tool: str(r, "tool"), Category: str(r, "category"), Reason: str(r, "reason"), Action: str(r, "action"), Retryable: boolean(r, "retryable")}
			a.recordToolFailure(failure, "preflight", "")
			if first == nil {
				first = failure
			}
		}
	}
	if first != nil {
		panic(first)
	}
}
