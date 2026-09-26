package app

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

func (a *App) lark(args ...string) M {
	args = a.larkArgs(args...)
	if a.CallLark != nil {
		return a.CallLark(args)
	}
	ensure(a.enabled(), "飞书未启用，运行 avatarthu login lark 可启用")
	return a.cliJSON(findExecutable("lark-cli", str(a.config(), "lark_cli")), args...)
}
func (a *App) larkArgs(args ...string) []string {
	if profile := str(a.config(), "lark_profile"); profile != "" {
		return append(append([]string{}, args...), "--profile", profile)
	}
	return args
}
func (a *App) cliJSON(exe string, args ...string) M {
	out, se, e := capture(a.Ctx, a.Root, 10*time.Minute, append([]string{exe}, args...)...)
	if e != nil {
		if len(se) > 0 {
			if err := attempt(func() {
				m := parseMap(se)
				panic(fmt.Errorf("Lark：%s", safeError(strDefault(obj(m, "error"), "message", "命令失败，请检查登录、权限或网络"))))
			}); err != nil {
				panic(err)
			}
		}
		panic(fmt.Errorf("Lark CLI 执行失败，请检查登录、网络和权限：%s", safeError(e)))
	}
	v := parseMap(out)
	// Lark management commands return a bare object; API shortcuts wrap data.
	if _, wrapped := v["ok"]; !wrapped && (len(args) > 0 && args[0] == "whoami" || len(args) > 1 && args[0] == "auth" && args[1] == "status") {
		ensure(v["error"] == nil, "Lark 身份检查失败")
		return v
	}
	ensure(boolean(v, "ok"), "Lark："+safeError(strDefault(obj(v, "error"), "message", "命令失败，请检查登录或权限")))
	return obj(v, "data")
}
func (a *App) send(card M, idem string) M {
	if a.SendCard != nil {
		return a.SendCard(card, idem)
	}
	cfg := a.config()
	args := []string{"im", "+messages-send", "--as", "bot", "--user-id", str(cfg, "lark_user_id"), "--msg-type", "interactive", "--content", string(jsonBytes(card))}
	if idem != "" {
		args = append(args, "--idempotency-key", fingerprint(idem)[:48])
	}
	v := a.lark(args...)
	ensure(str(v, "message_id") != "", "飞书未返回 message_id")
	return v
}
func (a *App) notifyOnce(key, text string) {
	defer a.lock("notifications", true)()
	p := a.data("notifications.json")
	sent := readMap(p)
	if sent[key] != nil {
		return
	}
	if a.enabled() {
		title, details, _ := strings.Cut(text, "\n")
		sent[key] = a.send(receiptCard("网络学堂 · 需要处理", title, details, false), key)
	} else {
		fmt.Println(text)
		sent[key] = M{"text": text, "recorded_at": stamp(), "channel": "local"}
	}
	writeJSON(p, sent)
}
func (a *App) sendNotices(notices []M, markRead func(M)) int {
	if !a.enabled() {
		return 0
	}
	p := a.data("notices.json")
	ack := readMap(p)
	count := 0
	for _, n := range notices {
		if !boolean(n, "unread") {
			continue
		}
		key := str(n, "course_id") + ":" + str(n, "id")
		parts := M{}
		for _, k := range []string{"course_id", "id", "title", "body", "date", "attachment_name"} {
			parts[k] = n[k]
		}
		version := fingerprint(parts)
		prev := obj(ack, key)
		if str(prev, "version") != version || len(obj(prev, "message")) == 0 {
			response := a.send(noticeCard(n), "notice:"+version)
			prev = M{"version": version, "message": response, "delivered_at": stamp()}
			ack[key] = prev
			writeJSON(p, ack)
			count++
		}
		markRead(n)
		prev["read_at"] = stamp()
		writeJSON(p, ack)
	}
	return count
}
func (a *App) loginLark(noStart bool, selectedProfile string) {
	previousConfig := a.config()
	profile := selectedProfile
	if profile == "" {
		profile = str(previousConfig, "lark_profile")
	}
	profileArgs := func(args ...string) []string {
		if profile != "" {
			args = append(args, "--profile", profile)
		}
		return args
	}
	exe := findExecutable("lark-cli", str(a.config(), "lark_cli"))
	if exe == "" {
		npm := findExecutable("npm", "")
		ensure(npm != "", "飞书为可选功能。安装 Lark CLI 需要 Node.js/npm；请先安装 Node.js LTS，再运行 avatarthu login lark。纯本地模式不需要它")
		prefix := filepath.Join(a.Root, "tools", "lark")
		fmt.Println("正在安装官方 Lark CLI 到 AvatarTHU 工具目录……")
		interactive(a.Ctx, a.Root, npm, "install", "--global", "--prefix", prefix, "@larksuite/cli@1.0.96")
		exe = findExecutable(filepath.Join(prefix, "bin", "lark-cli"), filepath.Join(prefix, "lark-cli.cmd"))
		ensure(exe != "", "未找到新安装的 Lark CLI")
	}
	status := M{}
	statusErr := attempt(func() { status = a.cliJSON(exe, profileArgs("auth", "status", "--json", "--verify")...) })
	if selectedProfile != "" && statusErr != nil {
		panic(statusErr)
	}
	if str(status, "appId") == "" {
		ensure(profile == "", "指定的 Lark profile 未配置，请先用 lark-cli profile list 检查")
		interactive(a.Ctx, a.Root, exe, "config", "init", "--new")
		status = a.cliJSON(exe, "auth", "status", "--json", "--verify")
	}
	identity := obj(obj(status, "identities"), "user")
	if !boolean(identity, "available") {
		interactive(a.Ctx, a.Root, append([]string{exe}, profileArgs("auth", "login", "--domain", "docs,drive,im")...)...)
	}
	var who M
	e := attempt(func() { who = a.cliJSON(exe, profileArgs("whoami", "--as", "user", "--json")...) })
	if e != nil {
		fmt.Println("已存飞书登录暂不可用，重新授权。 ")
		interactive(a.Ctx, a.Root, append([]string{exe}, profileArgs("auth", "login", "--domain", "docs,drive,im")...)...)
		who = a.cliJSON(exe, profileArgs("whoami", "--as", "user", "--json")...)
	}
	owner := str(obj(who, "onBehalfOf"), "openId")
	ensure(owner != "", "飞书未返回本人身份，配置未更改")
	if profile == "" {
		profile = str(who, "profile")
	}
	for _, key := range []string{"card.action.trigger", "im.message.receive_v1"} {
		a.cliJSON(exe, profileArgs("event", "consume", key, "--as", "bot", "--dry-run")...)
	}
	func() {
		defer a.lock("settings", true)()
		cfg := a.config()
		previous := str(cfg, "lark_user_id")
		// open_id is application-scoped. An explicit new profile binds its own
		// authenticated owner; old cards remain tied to their original app/message.
		explicitSwitch := selectedProfile != "" && selectedProfile != str(cfg, "lark_profile")
		ensure(previous == "" || previous == owner || len(a.tasks()) == 0 || explicitSwitch, "飞书账号与已有作业的审阅人不同，请切回原账号")
		merge(cfg, M{"lark_enabled": true, "lark_cli": exe, "lark_user_id": owner, "lark_profile": profile})
		a.saveConfig(cfg)
	}()
	if !noStart {
		a.serviceStart()
	}
	fmt.Println("飞书已启用，审阅文档和卡片会发给本人。")
}

// Resend only a frozen review. Persist the send intent before contacting Feishu,
// and keep old receipts; a transport retry reuses the same idempotency key.
func (a *App) resendReview(tid string) {
	defer a.lock(tid, true)()
	st := readMap(a.taskPath(tid))
	ensure(str(st, "status") == "awaiting" || str(st, "status") == "needs_student", "只有已完成、待审阅的作业可以补发通知")
	ensure(a.enabled(), "飞书未启用")
	review := a.publishCloud(st)
	ensure(boolean(review, "verified") && str(review, "url") != "", "当前版本审阅文档尚未发布完成")
	a.lark("docs", "+fetch", "--as", "user", "--doc", str(review, "url"), "--scope", "outline")
	request := obj(st, "resend_request")
	if str(request, "id") == "" || request["receipt"] != nil || number(request, "revision", -1) != number(st, "revision", 0) || str(request, "profile") != str(a.config(), "lark_profile") {
		request = M{"id": randomID(16), "requested_at": stamp(), "revision": st["revision"], "profile": str(a.config(), "lark_profile")}
		st["resend_request"] = request
		a.saveTask(st)
	}
	card := assignmentCard(st)
	if str(readMap(a.data("actions-health.json")), "state") != "ready" || str(readMap(a.data("messages-health.json")), "state") != "ready" {
		body := obj(card, "body")
		body["elements"] = append([]M{md("**连接提示：**回调尚未就绪。现在可以打开审阅文档；提交和修改按钮请等菜单栏显示飞书已连接后再操作。")}, objects(body["elements"])...)
	}
	receipt := a.send(card, "resend:"+str(request, "id"))
	history := objects(st["card_history"])
	if old := obj(obj(st, "deliveries"), "card"); len(old) > 0 {
		history = append(history, old)
	}
	st["card_history"] = history
	deliveries := obj(st, "deliveries")
	deliveries["card"] = receipt
	merge(st, M{"deliveries": deliveries, "card_message_id": receipt["message_id"], "chat_id": receipt["chat_id"]})
	merge(request, M{"receipt": receipt, "sent_at": stamp(), "profile": str(a.config(), "lark_profile")})
	a.saveTask(st)
	fmt.Println("已补发当前版本作业卡片，附最新审阅文档：" + str(review, "url"))
}
func (a *App) deliver(st M) {
	a.publishLocal(st)
	if !a.enabled() {
		merge(st, M{"status": deliveryStatus(st), "delivery_mode": "local"})
		a.saveTask(st)
		fmt.Println("本地审阅：" + str(st, "local_review"))
		return
	}
	a.publishCloud(st)
	deliveries := obj(st, "deliveries")
	if len(obj(deliveries, "card")) == 0 {
		r := a.send(assignmentCard(st), fmt.Sprintf("%s:%d:card", str(st, "task_id"), number(st, "revision", 1)))
		deliveries["card"] = r
		st["deliveries"] = deliveries
		st["card_message_id"] = r["message_id"]
		st["chat_id"] = r["chat_id"]
	}
	merge(st, M{"status": deliveryStatus(st), "delivery_mode": "lark", "links_synced": true})
	a.saveTask(st)
}
func deliveryStatus(st M) string {
	if boolean(st, "ready") {
		return "awaiting"
	}
	return "needs_student"
}
func (a *App) refreshCard(st M) {
	if str(st, "local_review") == "" || !exists(str(st, "local_review")) {
		a.publishLocal(st)
	}
	if !a.enabled() || boolean(st, "links_synced") && boolean(obj(st, "review_doc"), "verified") {
		return
	}
	if parseTime(str(st, "links_retry_at")).After(time.Now()) {
		return
	}
	st["links_retry_at"] = time.Now().Add(15 * time.Minute).Format(time.RFC3339)
	a.saveTask(st)
	if str(st, "card_message_id") == "" {
		a.deliver(st)
		return
	}
	a.publishCloud(st)
	a.lark("im", "messages", "patch", "--as", "bot", "--message-id", str(st, "card_message_id"), "--data", string(jsonBytes(M{"content": string(jsonBytes(assignmentCard(st)))})))
	st["links_synced"] = true
	a.saveTask(st)
}
func (a *App) pagedLark(args ...string) []M {
	all := []M{}
	token := ""
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		argv := append([]string{}, args...)
		if token != "" {
			argv = append(argv, "--page-token", token)
		}
		r := a.lark(argv...)
		all = append(all, objects(r["items"])...)
		if !boolean(r, "has_more") {
			return all
		}
		token = str(r, "page_token")
		ensure(token != "" && !seen[token], "飞书批注分页不完整，未开始修改")
		seen[token] = true
	}
	panic(fmt.Errorf("飞书批注数量超过处理上限，未开始修改"))
}
