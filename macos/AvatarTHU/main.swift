import AppKit
import Darwin

typealias Object = [String: Any]
let showStatusNotification = Notification.Name("com.local.avatarthu.menubar.showStatus")

func readObject(_ url: URL) -> Object {
    guard let data = try? Data(contentsOf: url),
          let value = try? JSONSerialization.jsonObject(with: data) as? Object else { return [:] }
    return value
}

func readObjects(_ url: URL) -> [Object] {
    guard let data = try? Data(contentsOf: url),
          let value = try? JSONSerialization.jsonObject(with: data) as? [Object] else { return [] }
    return value
}

struct Indicator {
    var state: String
    var text: String
    var detail: String = ""

    var color: NSColor {
        switch state {
        case "ready": return .systemGreen
        case "error": return .systemRed
        case "busy": return .systemBlue
        case "off": return .secondaryLabelColor
        default: return .systemOrange
        }
    }

    static func cli(_ tool: String, records: [Object], now: Date) -> Indicator {
        guard let record = records.first(where: { $0["tool"] as? String == tool }),
              let checked = dateValue(record["checked_at"]) else {
            return Indicator(state: "unknown", text: "尚未检查")
        }
        let detail = [record["reason"] as? String, record["action"] as? String,
                      record["version"] as? String].compactMap { $0 }.joined(separator: "\n")
        guard (-60...600).contains(now.timeIntervalSince(checked)) else {
            return Indicator(state: "stale", text: "检查记录待更新", detail: detail)
        }
        if record["usable"] as? Bool == true {
            return Indicator(state: "ready", text: "本地检查通过", detail: detail)
        }
        let reason = record["reason"] as? String ?? "检查失败"
        let text: String
        if reason.contains("未找到") { text = "未安装 / 路径无效" }
        else if reason.contains("登录") || reason.contains("授权") { text = "需要登录" }
        else if reason.contains("版本") { text = "需要更新 CLI" }
        else { text = "检查失败" }
        return Indicator(state: "error", text: text, detail: detail)
    }

    func withExecution(_ execution: Object, running: Bool) -> Indicator {
        switch execution["state"] as? String {
        case "failed":
            let reason = execution["reason"] as? String ?? "执行失败"
            let action = execution["action"] as? String ?? "查看错误详情后恢复"
            let retry = execution["retryable"] as? Bool == true ? "后台会重试，也可手动恢复。" : "自动重试已暂停；处理后点击检查并恢复。"
            let requested = execution["retry_requested_at"] as? String != nil
            return Indicator(state: "error", text: reason, detail: "最近执行异常：\(reason)\n\(action)\n\(requested ? "已请求恢复，等待实际执行成功。" : retry)\n本地检查：\(text)")
        case "running":
            if running && processAlive((execution["pid"] as? NSNumber)?.int32Value ?? 0) {
                let phase = execution["phase"] as? String == "reviewer" ? "正在独立复审" : "正在主写"
                return Indicator(state: "busy", text: phase, detail: "\(phase)；使用此 CLI 的默认模型。\n本地检查：\(text)")
            }
            return Indicator(state: "unknown", text: "上次执行已中断", detail: "执行进程已结束，等待后台从已完成阶段继续。\n本地检查：\(text)")
        default: return self
        }
    }
}

func sealImage() -> NSImage? {
    guard let url = Bundle.main.url(forResource: "TsinghuaSeal", withExtension: "svg"),
          let image = NSImage(contentsOf: url) else { return nil }
    image.size = NSSize(width: 19, height: 19)
    image.isTemplate = true
    image.accessibilityDescription = "清华大学校徽 · AvatarTHU"
    return image
}

func dateValue(_ value: Any?) -> Date? {
    guard let text = value as? String else { return nil }
    let format = ISO8601DateFormatter()
    format.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
    if let date = format.date(from: text) { return date }
    format.formatOptions = [.withInternetDateTime]
    return format.date(from: text)
}

func processAlive(_ pid: Int32, executable: URL? = nil) -> Bool {
    guard pid > 1, kill(pid, 0) == 0 || errno == EPERM else { return false }
    guard let executable = executable else { return true }
    // PROC_PIDPATHINFO_MAXSIZE is a C expression macro not imported by Swift.
    var buffer = [CChar](repeating: 0, count: 4 * Int(MAXPATHLEN))
    guard proc_pidpath(pid, &buffer, UInt32(buffer.count)) > 0 else { return false }
    let actual = URL(fileURLWithPath: String(cString: buffer)).resolvingSymlinksInPath().path
    return actual == executable.resolvingSymlinksInPath().path
}

func dataRoot() -> URL {
    let args = CommandLine.arguments
    if let i = args.firstIndex(of: "--home"), i + 1 < args.count {
        return URL(fileURLWithPath: args[i + 1]).resolvingSymlinksInPath()
    }
    if let path = ProcessInfo.processInfo.environment["AVATARTHU_HOME"], !path.isEmpty {
        return URL(fileURLWithPath: path).resolvingSymlinksInPath()
    }
    let home = FileManager.default.homeDirectoryForCurrentUser
    let alias = home.appendingPathComponent(".avatarthu")
    return (FileManager.default.fileExists(atPath: alias.path) ? alias : home.appendingPathComponent(".local/share/avatarthu")).resolvingSymlinksInPath()
}

struct Snapshot {
    var running = false
    var schedulerProblem = ""
    var pid: Int32 = 0
    var school = "尚未检查"
    var schoolOK = false
    var learn = Indicator(state: "unknown", text: "尚未检查")
    var lastSuccess: Date?
    var pollSeconds: Double = 43200
    var nextScan: Date?
    var lark = false
    var actions = false
    var messages = false
    var larkCLI = Indicator(state: "off", text: "未启用 · 本地审阅")
    var larkDetails = ""
    var claude = Indicator(state: "unknown", text: "尚未检查")
    var codex = Indicator(state: "unknown", text: "尚未检查")
    var cliCheckedAt: Date?
    var executions: [String: Object] = [:]
    var localCLI: [String: Indicator] = [:]
    var configuredMode = "claude-codex"
    var activeModes: [String] = []
    var busy = 0
    var queued = 0
    var awaiting = 0
    var attention = 0
    var reviews: [Object] = []
    var version = ""

    static func modeText(_ mode: String) -> String {
        switch mode {
        case "claude-codex": return "主写 Claude · 复审 Codex"
        case "codex-claude": return "主写 Codex · 复审 Claude"
        default: return "分工配置待检查"
        }
    }
    var modeText: String { Snapshot.modeText(configuredMode) }

    mutating func applyExecutionStatus() {
        claude = claude.withExecution(executions["claude"] ?? [:], running: running)
        codex = codex.withExecution(executions["codex"] ?? [:], running: running)
    }

    var kind: String {
        if !running { return "stopped" }
        if !schedulerProblem.isEmpty || !schoolOK || attention > 0 || (lark && (!actions || !messages)) || claude.state == "error" || codex.state == "error" { return "attention" }
        return busy > 0 ? "busy" : "running"
    }
    var headline: String {
        switch kind {
        case "stopped": return "后台已停止"
        case "attention": return "后台运行中 · 需要处理"
        case "busy": return "正在处理作业"
        default: return "后台运行中"
        }
    }

    static func load(root: URL, now: Date = Date()) -> Snapshot {
        var s = Snapshot()
        let config = readObject(root.appendingPathComponent("config.json"))
        let health = readObject(root.appendingPathComponent("data/daemon-health.json"))
        s.pid = (health["pid"] as? NSNumber)?.int32Value ?? 0
        s.running = health["state"] as? String == "running" && processAlive(s.pid, executable: root.appendingPathComponent("bin/avatarthu"))
        if s.running, (health["heartbeat_interval_seconds"] as? Int ?? 0) > 0 {
            if let heartbeat = dateValue(health["time"]), (-60...90).contains(now.timeIntervalSince(heartbeat)) {
                if let deadline = dateValue(health["deadline_at"]), now > deadline {
                    s.schedulerProblem = "调度阶段超时：\(health["phase"] as? String ?? "未知阶段")"
                }
            } else { s.schedulerProblem = "后台心跳未更新" }
        }
        s.version = health["version"] as? String ?? ""
        s.configuredMode = config["review_mode"] as? String ?? "claude-codex"
        for tool in ["claude", "codex"] { s.executions[tool] = readObject(root.appendingPathComponent("data/\(tool)-execution.json")) }
        let session = config["session"] as? String ?? root.appendingPathComponent("session.json").path
        let keepalive = readObject(URL(fileURLWithPath: session).deletingLastPathComponent().appendingPathComponent("keepalive-status.json"))
        s.lastSuccess = dateValue(keepalive["last_success"])
        switch keepalive["state"] as? String {
        case "valid":
            s.schoolOK = s.lastSuccess.map { (-60..<900).contains(now.timeIntervalSince($0)) } ?? false
            s.school = s.schoolOK ? "登录有效" : "保活记录待更新"
        case "expired": s.school = "登录已过期 · 请重新登录"
        case "unavailable": s.school = "暂时无法连接"
        default: break
        }
        let learnState = s.schoolOK ? "ready" : (["expired", "unavailable"].contains(keepalive["state"] as? String ?? "") ? "error" : "unknown")
        s.learn = Indicator(state: learnState, text: s.school, detail: "网络学堂登录及保活状态，每 10 分钟检查。")
        let cliRecords = readObjects(root.appendingPathComponent("data/cli-health.json"))
        s.claude = Indicator.cli("claude", records: cliRecords, now: now)
        s.codex = Indicator.cli("codex", records: cliRecords, now: now)
        s.localCLI = ["claude": s.claude, "codex": s.codex]
        let cliDates = ["claude", "codex"].compactMap { tool in dateValue(cliRecords.first { $0["tool"] as? String == tool }?["checked_at"]) }
        if cliDates.count == 2 { s.cliCheckedAt = cliDates.min() }
        s.pollSeconds = (config["poll_interval_seconds"] as? Double) ?? 43200
        let schedule = readObject(root.appendingPathComponent("data/schedule.json"))
        s.nextScan = dateValue(schedule["next_sync_retry_at"]) ?? dateValue(schedule["last_sync_at"]).map { $0.addingTimeInterval(s.pollSeconds) }
        s.lark = config["lark_enabled"] as? Bool ?? !(config["lark_user_id"] as? String ?? "").isEmpty
        func listener(_ name: String) -> Bool {
            let value = readObject(root.appendingPathComponent("data/\(name)-health.json"))
            return s.running && value["state"] as? String == "ready" && processAlive((value["pid"] as? NSNumber)?.int32Value ?? 0)
        }
        s.actions = listener("actions")
        s.messages = listener("messages")
        let listenerHealth = ["actions", "messages"].map { readObject(root.appendingPathComponent("data/\($0)-health.json")) }
        let failures = listenerHealth.filter { $0["state"] as? String != "ready" && !($0["reason"] as? String ?? "").isEmpty }
        s.larkDetails = zip(["卡片回调", "消息监听"], listenerHealth).map { name, value in
            let count = (value["consecutive_failures"] as? Int) ?? 0
            let reason = value["reason"] as? String ?? (value["state"] as? String ?? "尚未连接")
            let action = value["action"] as? String ?? ""
            return "\(name)：\(reason)\n连续失败：\(count) 次\n首次失败：\(value["first_failure_at"] as? String ?? "无")\n下次重试：\(value["retry_at"] as? String ?? "无")\n\(action)"
        }.joined(separator: "\n\n")
        s.larkDetails += "\n\nProfile：\(config["lark_profile"] as? String ?? "Lark CLI 默认配置")\n手动重连：avatarthu reconnect lark\n详细日志：\(root.appendingPathComponent("logs/daemon.log").path)"
        if s.lark {
            if s.actions && s.messages {
                s.larkCLI = Indicator(state: "ready", text: "卡片 / 消息已连接")
            } else if !s.running {
                s.larkCLI = Indicator(state: "off", text: "后台已停止 · 连接已停")
            } else if let failure = failures.first {
                s.larkCLI = Indicator(state: "error", text: failure["reason"] as? String ?? "飞书连接异常")
            } else {
                s.larkCLI = Indicator(state: "unknown", text: s.actions || s.messages ? "部分连接中断" : "等待连接 / 重连")
            }
            s.larkCLI.detail = "飞书卡片回调：\(s.actions ? "已连接" : "未连接")\n消息监听：\(s.messages ? "已连接" : "未连接")\n\n\(s.larkDetails)"
        }
        let taskURLs = (try? FileManager.default.contentsOfDirectory(at: root.appendingPathComponent("data/tasks"), includingPropertiesForKeys: nil)) ?? []
        for url in taskURLs where url.pathExtension == "json" {
            let task = readObject(url)
            switch task["status"] as? String {
            case "working", "reviewing", "rewriting":
                s.busy += 1
                let plan = task["execution_plan"] as? Object ?? [:]
                if let mode = plan["mode"] as? String, !s.activeModes.contains(mode) { s.activeModes.append(mode) }
            case "queued", "revision_ready", "delivery_pending": s.queued += 1
            case "awaiting": s.awaiting += 1; s.reviews.append(task)
            case "needs_student", "failed", "approval_invalid", "submission_unknown": s.attention += 1; s.reviews.append(task)
            default: break
            }
        }
        s.reviews.sort { ($0["updated_at"] as? String ?? "") > ($1["updated_at"] as? String ?? "") }
        s.applyExecutionStatus()
        return s
    }

    var diagnostic: Object {
        // Deliberately exclude account IDs, task text, document URLs and credentials.
        return ["state": kind, "daemon_running": running, "scheduler_problem": schedulerProblem, "pid": pid, "school": school,
                "school_valid": schoolOK, "poll_interval_seconds": pollSeconds,
                "lark_enabled": lark, "actions_ready": actions, "messages_ready": messages,
                "indicators": ["lark_cli": larkCLI.state, "learn": learn.state, "claude": claude.state, "codex": codex.state],
                "review_mode": configuredMode, "active_review_modes": activeModes,
                "working": busy, "queued": queued, "awaiting": awaiting, "attention": attention]
    }
}

final class MenuBar: NSObject, NSApplicationDelegate, NSMenuDelegate {
    let root = dataRoot()
    var item: NSStatusItem!
    var menu = NSMenu()
    var timer: Timer?
    var snapshot = Snapshot()
    var busyAction = false
    var lockFD: Int32 = -1
    var ownsLock = false
    var detailWindow: NSWindow?
    var detailText: NSTextView?
    var activeProcess: Process?
    var cliProbe: Process?
    var lastCLIProbe: Date?
    var cliProbeError: String?
    var indicatorViews: [String: (dot: NSView, label: NSTextField, view: NSView)] = [:]

    func applicationDidFinishLaunching(_ notification: Notification) {
        let locks = root.appendingPathComponent("data/locks")
        try? FileManager.default.createDirectory(at: locks, withIntermediateDirectories: true)
        lockFD = Darwin.open(locks.appendingPathComponent("menubar.lock").path, O_CREAT | O_RDWR | O_CLOEXEC, S_IRUSR | S_IWUSR)
        guard lockFD >= 0, flock(lockFD, LOCK_EX | LOCK_NB) == 0 else {
            if !CommandLine.arguments.contains("--home") {
                DistributedNotificationCenter.default().postNotificationName(showStatusNotification, object: root.path, userInfo: nil, deliverImmediately: true)
            }
            NSApp.terminate(nil)
            return
        }
        ownsLock = true
        DistributedNotificationCenter.default().addObserver(self, selector: #selector(showStatus), name: showStatusNotification, object: root.path)
        menu.delegate = self
        menu.autoenablesItems = false
        item = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
        item.menu = menu
        item.button?.font = .systemFont(ofSize: 12, weight: .medium)
        item.button?.setAccessibilityLabel("AvatarTHU 常驻状态")
        item.button?.image = sealImage()
        item.button?.imagePosition = .imageLeading
        refresh()
        timer = Timer(timeInterval: 15, repeats: true) { [weak self] _ in self?.refresh() }
        timer?.tolerance = 3
        RunLoop.main.add(timer!, forMode: .common)
        NSWorkspace.shared.notificationCenter.addObserver(self, selector: #selector(refresh), name: NSWorkspace.didWakeNotification, object: nil)
        // Opening the app in Finder also provides an accessible status window.
        // LaunchAgent starts pass --home and remain unobtrusive in the menu bar.
        if !CommandLine.arguments.contains("--home") { showStatus() }
    }

    func applicationShouldHandleReopen(_ sender: NSApplication, hasVisibleWindows flag: Bool) -> Bool {
        showStatus()
        return true
    }

    func applicationWillTerminate(_ notification: Notification) {
        timer?.invalidate()
        activeProcess?.terminate()
        cliProbe?.terminate()
        if ownsLock { writeHealth("stopped") }
        if lockFD >= 0 { close(lockFD) }
    }

    func writeHealth(_ state: String) {
        let value: Object = ["state": state, "pid": ProcessInfo.processInfo.processIdentifier,
                             "time": ISO8601DateFormatter().string(from: Date())]
        if let data = try? JSONSerialization.data(withJSONObject: value) {
            try? data.write(to: root.appendingPathComponent("data/menubar-health.json"), options: .atomic)
        }
    }

    @objc func refresh() {
        snapshot = Snapshot.load(root: root)
        snapshot.claude = snapshot.localCLI["claude"] ?? snapshot.claude
        snapshot.codex = snapshot.localCLI["codex"] ?? snapshot.codex
        checkCLIsIfNeeded()
        if let error = cliProbeError {
            snapshot.claude = Indicator(state: "error", text: "检查未完成", detail: error)
            snapshot.codex = snapshot.claude
        } else if cliProbe != nil {
            if snapshot.claude.state != "ready" && snapshot.claude.state != "busy" { snapshot.claude = Indicator(state: "checking", text: "正在检查…") }
            if snapshot.codex.state != "ready" && snapshot.codex.state != "busy" { snapshot.codex = Indicator(state: "checking", text: "正在检查…") }
        }
        // A successful local login check must not hide a failed model request.
        snapshot.applyExecutionStatus()
        let fallback = item.button?.image == nil ? "THU" : ""
        item.button?.title = fallback + (snapshot.awaiting > 0 ? " \(snapshot.awaiting)" : "")
        item.button?.toolTip = "AvatarTHU · \(snapshot.headline)\n\(snapshot.modeText)\n网络学堂：\(snapshot.school)\n待审阅 \(snapshot.awaiting) · 需处理 \(snapshot.attention)"
        for (title, indicator) in [("Lark CLI", snapshot.larkCLI), ("Learn · 网络学堂", snapshot.learn), ("Claude", snapshot.claude), ("Codex", snapshot.codex)] {
            guard let views = indicatorViews[title] else { continue }
            views.dot.layer?.backgroundColor = indicator.color.cgColor
            views.label.stringValue = indicator.text
            views.view.toolTip = indicator.detail
            views.view.setAccessibilityLabel("\(title)：\(indicator.text)")
        }
        writeHealth("running")
    }

    func menuNeedsUpdate(_ menu: NSMenu) { refresh(); rebuild() }

    // Only local version/auth/help commands; never launch an inference request.
    // Cache across refreshes and app launches, and keep the menu responsive.
    func checkCLIsIfNeeded(force: Bool = false) {
        guard cliProbe == nil else { return }
        let now = Date()
        if !force, [lastCLIProbe, snapshot.cliCheckedAt].compactMap({ $0 }).contains(where: { (0..<300).contains(now.timeIntervalSince($0)) }) { return }
        lastCLIProbe = now
        cliProbeError = nil
        let process = Process()
        process.executableURL = root.appendingPathComponent("bin/avatarthu")
        process.arguments = ["doctor"]
        process.currentDirectoryURL = root
        var env = ProcessInfo.processInfo.environment
        env["AVATARTHU_HOME"] = root.path
        process.environment = env
        process.standardInput = FileHandle.nullDevice
        process.standardOutput = FileHandle.nullDevice
        process.standardError = FileHandle.nullDevice
        process.terminationHandler = { [weak self] completed in
            DispatchQueue.main.async {
                guard let self = self else { return }
                self.cliProbe = nil
                let records = readObjects(self.root.appendingPathComponent("data/cli-health.json"))
                let completedCheck = ["claude", "codex"].allSatisfy { tool in
                    guard let checked = dateValue(records.first { $0["tool"] as? String == tool }?["checked_at"]) else { return false }
                    return checked >= now.addingTimeInterval(-1)
                }
                if !completedCheck { self.cliProbeError = "检查未完成（退出码 \(completed.terminationStatus)）；可运行 avatarthu doctor 查看原因。" }
                self.refresh()
            }
        }
        do {
            try process.run()
            cliProbe = process
        } catch {
            cliProbeError = "无法运行 avatarthu doctor：\(error.localizedDescription)"
        }
    }

    @objc func refreshStatus() { checkCLIsIfNeeded(force: true); refresh() }

    func indicatorRow(_ title: String, _ indicator: Indicator) {
        let entry = NSMenuItem(title: "\(title)：\(indicator.text)", action: nil, keyEquivalent: "")
        let view = NSView(frame: NSRect(x: 0, y: 0, width: 360, height: 27))
        let dot = NSView(frame: NSRect(x: 16, y: 9, width: 8, height: 8))
        dot.wantsLayer = true
        dot.layer?.cornerRadius = 4
        dot.layer?.backgroundColor = indicator.color.cgColor
        let name = NSTextField(labelWithString: title)
        name.font = .systemFont(ofSize: 12, weight: .medium)
        name.frame = NSRect(x: 36, y: 5, width: 115, height: 18)
        let status = NSTextField(labelWithString: indicator.text)
        status.font = .systemFont(ofSize: 12)
        status.textColor = .secondaryLabelColor
        status.alignment = .right
        status.frame = NSRect(x: 154, y: 5, width: 190, height: 18)
        view.addSubview(dot); view.addSubview(name); view.addSubview(status)
        view.toolTip = indicator.detail.isEmpty ? entry.title : indicator.detail
        view.setAccessibilityElement(true)
        view.setAccessibilityRole(.staticText)
        view.setAccessibilityLabel(entry.title)
        entry.view = view
        indicatorViews[title] = (dot, status, view)
        menu.addItem(entry)
    }

    @discardableResult func row(_ title: String, action: Selector? = nil, symbol: String? = nil, to targetMenu: NSMenu? = nil) -> NSMenuItem {
        let result = NSMenuItem(title: title, action: action, keyEquivalent: "")
        result.target = self
        result.isEnabled = action != nil && !busyAction
        if action == nil {
            // Native disabled menu titles are too faint for monitoring data.
            // A label view stays readable without looking like an action.
            let label = NSTextField(labelWithString: title)
            label.font = .systemFont(ofSize: title == "AvatarTHU" ? 14 : 12, weight: title == "AvatarTHU" ? .semibold : .regular)
            label.textColor = .labelColor
            let width = min(440, max(320, label.intrinsicContentSize.width + 58))
            let view = NSView(frame: NSRect(x: 0, y: 0, width: width, height: 23))
            label.frame = NSRect(x: 36, y: 3, width: width - 48, height: 18)
            view.addSubview(label)
            if let symbol = symbol {
                let image = NSImageView(frame: NSRect(x: 14, y: 4, width: 15, height: 15))
                image.image = NSImage(systemSymbolName: symbol, accessibilityDescription: nil)
                view.addSubview(image)
            }
            result.view = view
        } else if let symbol = symbol { result.image = NSImage(systemSymbolName: symbol, accessibilityDescription: nil) }
        (targetMenu ?? menu).addItem(result)
        return result
    }

    func timeText(_ date: Date?) -> String {
        guard let date = date else { return "尚无记录" }
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "zh_CN")
        formatter.dateFormat = Calendar.current.isDateInToday(date) ? "'今天' HH:mm" : "MM-dd HH:mm"
        return formatter.string(from: date)
    }

    func rebuild() {
        menu.removeAllItems()
        indicatorViews.removeAll()
        let heading = row("AvatarTHU")
        heading.attributedTitle = NSAttributedString(string: "AvatarTHU", attributes: [.font: NSFont.systemFont(ofSize: 15, weight: .semibold), .foregroundColor: NSColor.labelColor])
        row(snapshot.headline + (snapshot.running ? " · PID \(snapshot.pid)" : ""))
        if !snapshot.schedulerProblem.isEmpty { row(snapshot.schedulerProblem + "…", action: #selector(showStatus), symbol: "exclamationmark.triangle") }
        if snapshot.activeModes.contains(where: { $0 != snapshot.configuredMode }) {
            for mode in snapshot.activeModes { row("当前作业 · \(Snapshot.modeText(mode))") }
            row("后续版本 · \(snapshot.modeText)")
        } else { row(snapshot.modeText) }
        menu.addItem(.separator())
        indicatorRow("Lark CLI", snapshot.larkCLI)
        indicatorRow("Learn · 网络学堂", snapshot.learn)
        indicatorRow("Claude", snapshot.claude)
        indicatorRow("Codex", snapshot.codex)
        if snapshot.lark && (!snapshot.actions || !snapshot.messages) {
            row("飞书连接异常详情…", action: #selector(showLarkFailure), symbol: "exclamationmark.bubble")
            row("重连飞书", action: #selector(reconnectLark), symbol: "arrow.clockwise")
        }
        for tool in ["claude", "codex"] {
            guard let failure = snapshot.executions[tool], failure["state"] as? String == "failed" else { continue }
            let title = tool == "claude" ? "Claude" : "Codex"
            let details = row("\(title) 执行异常详情…", action: #selector(showFailure(_:)), symbol: "exclamationmark.bubble")
            details.representedObject = tool
            let retry = row("检查并恢复 \(title)", action: #selector(retryTool(_:)), symbol: "arrow.clockwise")
            retry.representedObject = tool
        }
        menu.addItem(.separator())
        row("最近保活 · \(timeText(snapshot.lastSuccess))（每 10 分钟）")
        let interval = snapshot.pollSeconds >= 3600 ? String(format: "%g 小时", snapshot.pollSeconds / 3600) : String(format: "%g 分钟", snapshot.pollSeconds / 60)
        row("课程扫描 · 每 \(interval)")
        let next = snapshot.nextScan.map { $0 <= Date() ? "已到期，等待调度" : timeText($0) } ?? "首次启动时扫描"
        row("下次扫描 · \(snapshot.running ? next : "恢复后台后检查")")
        menu.addItem(.separator())
        row("作业 · \(snapshot.busy) 处理中 / \(snapshot.queued) 排队 / \(snapshot.awaiting) 待审阅 / \(snapshot.attention) 需处理")
        if !snapshot.reviews.isEmpty {
            let reviews = NSMenu()
            reviews.autoenablesItems = false
            for task in snapshot.reviews.prefix(15) {
                let title = String((task["title"] as? String ?? "作业").replacingOccurrences(of: "\n", with: " ").prefix(48))
                let entry = row(title, action: #selector(openReview(_:)), symbol: "doc.text", to: reviews)
                entry.representedObject = task
            }
            let entry = row("打开审阅文档", symbol: "doc.text.magnifyingglass")
            entry.view = nil
            entry.image = NSImage(systemSymbolName: "doc.text.magnifyingglass", accessibilityDescription: nil)
            entry.submenu = reviews
            entry.isEnabled = true
        }
        row("打开课程目录", action: #selector(openCourses), symbol: "folder")
        row("查看运行日志", action: #selector(openLog), symbol: "doc.plaintext")
        row("查看详细状态…", action: #selector(showStatus), symbol: "info.circle")
        menu.addItem(.separator())
        if busyAction { row("正在执行操作…") }
        row(snapshot.running ? "停止后台" : "启动后台", action: #selector(toggleService), symbol: snapshot.running ? "stop.circle" : "play.circle")
        row("立即检查登录与保活", action: #selector(keepalive), symbol: "arrow.clockwise")
        row("重新登录网络学堂…", action: #selector(login), symbol: "person.badge.key")
        row("刷新状态与 CLI 检查", action: #selector(refreshStatus), symbol: "arrow.triangle.2.circlepath")
        menu.addItem(.separator())
        let quit = row("退出菜单栏（后台继续运行）", action: #selector(quitApp), symbol: "xmark.circle")
        quit.isEnabled = !busyAction
    }

    @objc func openCourses() { NSWorkspace.shared.open(root.appendingPathComponent("courses")) }
    @objc func openLog() { NSWorkspace.shared.open(root.appendingPathComponent("logs/daemon.log")) }
    @objc func showLarkFailure() { showText("飞书连接状态", snapshot.larkDetails) }
    @objc func reconnectLark() { execute(["reconnect", "lark"], title: "重连飞书", openingText: "正在请求后台重连本项目的监听；连接成功后指示灯恢复绿色。") }
    @objc func showFailure(_ sender: NSMenuItem) {
        guard let tool = sender.representedObject as? String, let failure = snapshot.executions[tool] else { return }
        let role = failure["phase"] as? String == "reviewer" ? "独立复审" : (failure["phase"] as? String == "writer" ? "主写" : "启动检查")
        let retry = failure["retryable"] as? Bool == true ? "后台 15 分钟后重试，也可以在菜单中手动恢复。" : "自动重试已暂停；处理后在菜单中点击检查并恢复。"
        showText("\(tool.capitalized) 执行异常", "阶段：\(role)\n时间：\(timeText(dateValue(failure["time"])))\n原因：\(failure["reason"] as? String ?? "执行失败")\n\n处理方式：\(failure["action"] as? String ?? "检查 CLI 设置")\n\(retry)\n\n错误日志：\(failure["log"] as? String ?? "无独立日志，请查看后台日志")\n\n恢复命令：avatarthu retry --tool \(tool)")
    }
    @objc func retryTool(_ sender: NSMenuItem) {
        guard let tool = sender.representedObject as? String, ["claude", "codex"].contains(tool) else { return }
        execute(["retry", "--tool", tool], title: "检查并恢复 \(tool.capitalized)", openingText: "正在检查两个 CLI 的本地可用性，通过后将失败作业排队，由后台继续主写或复审。")
    }
    @objc func openReview(_ sender: NSMenuItem) {
        guard let task = sender.representedObject as? Object else { return }
        if let doc = task["review_doc"] as? Object, let text = doc["url"] as? String,
           let url = URL(string: text), url.scheme == "https" { NSWorkspace.shared.open(url); return }
        if let path = task["local_review"] as? String {
            let url = URL(fileURLWithPath: path).resolvingSymlinksInPath()
            if url.path.hasPrefix(root.path + "/"), FileManager.default.fileExists(atPath: url.path) { NSWorkspace.shared.open(url); return }
        }
        showText("审阅文档尚未生成", "请查看详细状态中的作业错误或等待当前处理完成。")
    }
    @objc func showStatus() { execute(["status"], title: "AvatarTHU 详细状态") }
    @objc func keepalive() { execute(["keepalive", "run"], title: "网络学堂保活") }
    @objc func login() { execute(["login", "thu"], title: "登录网络学堂", openingText: "请在打开的浏览器中完成本人认证。登录窗口完成后会自动更新状态。") }
    @objc func toggleService() { execute(["service", snapshot.running ? "stop" : "start"], title: snapshot.running ? "停止后台" : "启动后台") }
    @objc func quitApp() { NSApp.terminate(nil) }
    @objc func openStatusMenu() {
        refresh(); rebuild()
        guard let view = detailWindow?.contentView else { return }
        menu.popUp(positioning: nil, at: NSPoint(x: view.bounds.width - 20, y: view.bounds.height), in: view)
    }

    func showText(_ title: String, _ text: String) {
        if detailWindow == nil {
            let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 660, height: 440), styleMask: [.titled, .closable, .resizable], backing: .buffered, defer: false)
            window.isReleasedWhenClosed = false
            let scroll = NSScrollView(frame: window.contentView!.bounds)
            scroll.autoresizingMask = [.width, .height]
            scroll.hasVerticalScroller = true
            let textView = NSTextView(frame: scroll.bounds)
            textView.isEditable = false
            textView.isSelectable = true
            textView.font = .monospacedSystemFont(ofSize: 12, weight: .regular)
            textView.textContainerInset = NSSize(width: 16, height: 16)
            textView.autoresizingMask = [.width]
            textView.textContainer?.widthTracksTextView = true
            scroll.documentView = textView
            window.contentView?.addSubview(scroll)
            let accessory = NSTitlebarAccessoryViewController()
            accessory.layoutAttribute = .right
            let menuButton = NSButton(title: "状态菜单", target: self, action: #selector(openStatusMenu))
            menuButton.frame = NSRect(x: 0, y: 0, width: 90, height: 26)
            menuButton.bezelStyle = .rounded
            accessory.view = menuButton
            window.addTitlebarAccessoryViewController(accessory)
            window.center()
            detailWindow = window; detailText = textView
        }
        detailWindow?.title = title
        detailText?.string = text
        detailWindow?.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
    }

    func execute(_ arguments: [String], title: String, openingText: String = "正在执行，请稍候…") {
        guard !busyAction else { return }
        busyAction = true
        showText(title, openingText)
        let process = Process()
        process.executableURL = root.appendingPathComponent("bin/avatarthu")
        process.arguments = arguments
        process.currentDirectoryURL = root
        var env = ProcessInfo.processInfo.environment
        env["AVATARTHU_HOME"] = root.path
        process.environment = env
        // File output avoids a full pipe blocking a long authentication operation.
        let output = root.appendingPathComponent("logs/menubar-command.log")
        do {
            try FileManager.default.createDirectory(at: output.deletingLastPathComponent(), withIntermediateDirectories: true)
            try Data().write(to: output, options: .atomic)
            try FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: output.path)
            let handle = try FileHandle(forWritingTo: output)
            process.standardOutput = handle; process.standardError = handle
            process.standardInput = FileHandle.nullDevice
            process.terminationHandler = { [weak self] completed in
                try? handle.close()
                let data = (try? Data(contentsOf: output)) ?? Data()
                let text = String(decoding: data.suffix(128 * 1024), as: UTF8.self)
                DispatchQueue.main.async {
                    guard let self = self else { return }
                    self.activeProcess = nil; self.busyAction = false
                    self.refresh()
                    self.showText(title, (completed.terminationStatus == 0 ? "" : "操作未完成（退出码 \(completed.terminationStatus)）\n\n") + text)
                }
            }
            try process.run()
            activeProcess = process
        } catch {
            busyAction = false
            showText(title, "无法执行命令：\(error.localizedDescription)\n请检查 AvatarTHU 安装，或在终端运行 avatarthu status。")
        }
    }
}

if CommandLine.arguments.contains("--snapshot") {
    let data = try JSONSerialization.data(withJSONObject: Snapshot.load(root: dataRoot()).diagnostic, options: [.sortedKeys])
    print(String(decoding: data, as: UTF8.self))
} else {
    let app = NSApplication.shared
    app.setActivationPolicy(.accessory)
    let delegate = MenuBar()
    app.delegate = delegate
    app.run()
}
