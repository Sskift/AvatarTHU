package app

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func Main(args []string) (code int) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintln(os.Stderr, safeError(r))
			code = 1
		}
	}()
	if len(args) == 0 || args[0] == "--help" || args[0] == "help" {
		printHelp()
		return 0
	}
	if args[0] == "--version" || args[0] == "version" {
		fmt.Println("AvatarTHU " + Version + " (Go native)")
		return 0
	}
	a := New(ctx, "")
	if args[0] == "daemon" {
		f := flag.NewFlagSet("daemon", flag.ContinueOnError)
		root := f.String("home", "", "数据目录")
		check(f.Parse(args[1:]))
		if *root != "" {
			a = New(ctx, *root)
		}
		mkdir(filepath.Join(a.Root, "logs"))
		p := filepath.Join(a.Root, "logs", "daemon.log")
		if st, e := os.Stat(p); e == nil && st.Size() > 10<<20 {
			_ = os.Remove(p + ".1")
			check(os.Rename(p, p+".1"))
		}
		log, e := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		check(e)
		defer log.Close()
		os.Stdout, os.Stderr = log, log
		fmt.Println(stamp(), "启动 AvatarTHU", Version)
		a.daemon()
		return 0
	}
	mkdir(a.Root)
	switch args[0] {
	case "init":
		f := flag.NewFlagSet("init", flag.ContinueOnError)
		noLogin := f.Bool("no-login", false, "稍后登录")
		noStart := f.Bool("no-start", false, "暂不启动后台")
		check(f.Parse(args[1:]))
		a.init(*noLogin, *noStart)
	case "configure":
		a.configure(args[1:])
	case "status":
		a.status()
	case "doctor":
		okay := true
		for _, r := range a.inspectPair(a.pairing()) {
			fmt.Printf("%s：%s %s\n  %s\n", str(r, "tool"), str(r, "reason"), str(r, "version"), str(r, "action"))
			okay = okay && boolean(r, "usable")
		}
		if !okay {
			return 1
		}
	case "tools":
		printTools()
	case "login":
		provider := "thu"
		rest := args[1:]
		if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
			provider = rest[0]
			rest = rest[1:]
		}
		f := flag.NewFlagSet("login", flag.ContinueOnError)
		importOnly := f.Bool("import-only", false, "只导入 macOS Chrome 会话")
		noStart := f.Bool("no-start", false, "登录飞书后暂不启动后台")
		check(f.Parse(rest))
		if provider == "thu" {
			a.loginTHU(*importOnly)
		} else if provider == "lark" {
			ensure(!*importOnly, "--import-only 只适用于网络学堂")
			a.loginLark(*noStart)
		} else {
			panic(fmt.Errorf("登录对象应是 thu 或 lark"))
		}
	case "notifications":
		ensure(len(args) == 2 && (args[1] == "on" || args[1] == "off"), "用法：avatarthu notifications on|off")
		if args[1] == "on" {
			a.loginLark(false)
		} else {
			func() {
				defer a.lock("settings", true)()
				cfg := a.config()
				cfg["lark_enabled"] = false
				a.saveConfig(cfg)
			}()
			fmt.Println("已关闭飞书推送，后台将在 15 秒内停止回调连接；继续本地归档和复审。")
		}
	case "keepalive":
		action := "status"
		if len(args) > 1 {
			action = args[1]
		}
		ensure(action == "status" || action == "run", "用法：avatarthu keepalive status|run")
		state := readMap(filepath.Join(filepath.Dir(a.session()), "keepalive-status.json"))
		if action == "run" {
			state = a.keepalive(true, true)
		}
		fmt.Println(string(jsonBytes(state)))
		if action == "run" && str(state, "state") != "valid" {
			return 1
		}
	case "run":
		f := flag.NewFlagSet("run", flag.ContinueOnError)
		syncOnly := f.Bool("sync-only", false, "只扫描下载")
		tick := f.Bool("tick", false, "仅在到期时扫描")
		task := f.String("task", "", "只处理指定作业")
		check(f.Parse(args[1:]))
		if !a.run(*tick, *syncOnly, *task) {
			return 1
		}
	case "revise":
		ensure(len(args) >= 2, "用法：avatarthu revise 作业编号 --feedback 修改意见")
		f := flag.NewFlagSet("revise", flag.ContinueOnError)
		feedback := f.String("feedback", "", "修改意见")
		check(f.Parse(args[2:]))
		ensure(strings.TrimSpace(*feedback) != "", "修改意见不能为空")
		defer a.lock(args[1], true)()
		st := readMap(a.taskPath(args[1]))
		switch str(st, "status") {
		case "awaiting", "needs_student", "approval_invalid":
		default:
			panic(fmt.Errorf("此作业当前不能接收新修改"))
		}
		merge(st, M{"status": "revision_ready", "feedback": *feedback, "approval_event": nil})
		a.saveTask(st)
		fmt.Println("修改已排队，后台下一分钟开始处理；也可运行 avatarthu run。")
	case "service":
		ensure(len(args) == 2, "用法：avatarthu service start|stop|status")
		switch args[1] {
		case "start":
			a.serviceStart()
		case "stop":
			a.serviceStop()
		case "status":
			a.status()
		default:
			panic(fmt.Errorf("未知服务操作"))
		}
	case "menubar":
		action := "status"
		if len(args) == 2 {
			action = args[1]
		}
		ensure(len(args) <= 2, "用法：avatarthu menubar start|stop|status")
		a.menubarCommand(action)
	case "uninstall":
		a.serviceStop()
		a.stopMenubar()
	default:
		panic(fmt.Errorf("未知命令 %s，运行 avatarthu --help", args[0]))
	}
	return 0
}
func duration(raw string) int {
	m := regexp.MustCompile(`^\s*(\d+(?:\.\d+)?)\s*([smhd]?)\s*$`).FindStringSubmatch(strings.ToLower(raw))
	ensure(m != nil, "轮询间隔请写 30m、12h 或 1d")
	n, e := strconv.ParseFloat(m[1], 64)
	check(e)
	unit := m[2]
	if unit == "" {
		unit = "h"
	}
	n *= map[string]float64{"s": 1, "m": 60, "h": 3600, "d": 86400}[unit]
	ensure(n >= 60 && n <= 365*86400, "轮询间隔应在 1 分钟至 365 天之间")
	return int(n)
}
func (a *App) configure(args []string) {
	f := flag.NewFlagSet("configure", flag.ContinueOnError)
	mode := f.String("mode", "", "claude-codex 或 codex-claude")
	poll := f.String("poll-interval", "", "30m、12h、1d")
	rounds := f.Int("max-review-rounds", -1, "每版最多复审轮数，0 为不限")
	check(f.Parse(args))
	changes := M{}
	if *mode != "" {
		ensure(*mode == "claude-codex" || *mode == "codex-claude", "只支持 claude-codex 或 codex-claude")
		changes["review_mode"] = *mode
	}
	if *poll != "" {
		changes["poll_interval_seconds"] = duration(*poll)
	}
	f.Visit(func(v *flag.Flag) {
		if v.Name == "max-review-rounds" {
			ensure(*rounds >= 0, "复审轮数不能为负数")
			changes["max_review_rounds"] = *rounds
		}
	})
	func() {
		defer a.lock("settings", true)()
		cfg := a.config()
		merge(cfg, changes)
		for _, tool := range []string{"claude", "codex"} {
			if p := findExecutable(tool, ""); p != "" {
				cfg[tool+"_cli"] = p
			}
		}
		a.saveConfig(cfg)
	}()
	p := a.pairing()
	fmt.Printf("主写：%s；复审：%s；分别使用各自 CLI 默认模型。\n课程轮询：每 %g 小时；同一进程每 10 分钟保活。\n每版复审上限：%d（0 为不限）。\n", str(p, "writer"), str(p, "reviewer"), float64(number(a.config(), "poll_interval_seconds", 43200))/3600, number(p, "max_review_rounds", 3))
	if len(changes) > 0 {
		fmt.Println("设置已保存；轮询下一次调度生效，复审分工从下一版作业开始。")
	}
}
func (a *App) status() {
	fmt.Printf("AvatarTHU %s · Go native\n数据目录：%s\n后台服务已注册：%t\n", Version, a.Root, a.serviceLoaded())
	h := readMap(a.data("daemon-health.json"))
	fmt.Printf("调度进程：%s PID=%d，更新时间 %s\n", strDefault(h, "state", "未启动"), number(h, "pid", 0), str(h, "time"))
	k := readMap(filepath.Join(filepath.Dir(a.session()), "keepalive-status.json"))
	fmt.Printf("网络学堂：%s · %s\n保活：每 10 分钟；最近检查 %s，最近成功 %s\n", strDefault(k, "state", "尚未检查"), str(k, "message"), str(k, "checked_at"), str(k, "last_success"))
	fmt.Printf("课程扫描：每 %g 小时；下次 %s\n", float64(number(a.config(), "poll_interval_seconds", 43200))/3600, a.nextScan(readMap(a.data("schedule.json"))).In(beijing).Format(time.RFC3339))
	fmt.Printf("飞书：%t\n", a.enabled())
	for _, name := range []string{"actions", "messages"} {
		if a.enabled() {
			m := readMap(a.data(name + "-health.json"))
			fmt.Printf("%s 连接：%s · %s\n", name, strDefault(m, "state", "未启动"), str(m, "time"))
		}
	}
	p := a.pairing()
	fmt.Printf("主写 %s / 独立复审 %s；模型沿用各自默认配置。\n", str(p, "writer"), str(p, "reviewer"))
	for _, st := range a.tasks() {
		fmt.Printf("\n%s · %s · r%d · %s\n", str(st, "task_id"), str(st, "title"), number(st, "revision", 1), str(st, "status"))
		if str(st, "error") != "" {
			fmt.Println("  " + safeError(str(st, "error")))
		}
		if url := str(obj(st, "review_doc"), "url"); url != "" {
			fmt.Println("  云文档：" + url)
		}
		if str(st, "local_review") != "" {
			fmt.Println("  本地审阅：" + str(st, "local_review"))
		}
	}
}
func printHelp() {
	fmt.Println(`AvatarTHU — 原生课程助手

avatarthu init [--no-login] [--no-start]   安装命令并初始化
avatarthu login thu|lark                   登录网络学堂；飞书可选
avatarthu configure --mode claude-codex     Claude 主写 / Codex 复审
avatarthu configure --mode codex-claude     Codex 主写 / Claude 复审
avatarthu configure --poll-interval 12h     设置课程轮询，保活固定 10 分钟
avatarthu configure --max-review-rounds 3   复审不通过自动重写，0 表示不限
avatarthu service start|stop|status         管理后台进程
avatarthu menubar start|stop|status         macOS 原生菜单栏监控
avatarthu run [--sync-only] [--task ID]     立即扫描或处理
avatarthu revise ID --feedback "修改意见"  本地提出修改
avatarthu keepalive status|run              查看或立即保活
avatarthu notifications on|off              可选飞书推送
avatarthu doctor                           检查两种执行器的安装和登录
avatarthu tools                            查看外部工具安装方式
avatarthu status                           查看作业、审阅文档和服务状态
avatarthu daemon                           前台运行同一个后台循环
avatarthu uninstall                        停用服务，保留全部数据`)
}
func printTools() {
	fmt.Println(`AvatarTHU 本身无额外运行依赖，不需要 Python、Go、Node.js 或 Java。

完成作业并交叉复审需要本机 Claude Code 和 Codex CLI：
  Claude Code: https://code.claude.com/docs/en/setup
  Codex CLI:   https://github.com/openai/codex#quickstart
  若已安装 Node.js/npm，可执行：
    npm install -g @anthropic-ai/claude-code @openai/codex
  分别运行 claude / codex 完成登录，再运行 avatarthu doctor。
  两边沿用自己的默认模型配置，AvatarTHU 不更改模型。

网络学堂登录使用已安装的 Chrome；Windows 也可使用 Edge。
不需要 Selenium、ChromeDriver；登录窗口仅在认证时打开。

飞书为可选项。运行 avatarthu login lark 会复用已有 Lark CLI；
若未安装，会通过本机 npm 将 @larksuite/cli 安装在本项目数据目录。
只有此可选安装方式需要 Node.js LTS：https://nodejs.org/

具体作业所需的编译器、LaTeX 或实验软件取决于题目，未安装或无法验证的
部分会作为阻塞事项交给你处理，不会虚构运行结果。`)
}
