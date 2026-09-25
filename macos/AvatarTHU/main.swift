import AppKit
import Darwin

typealias Object = [String: Any]
let showStatusNotification = Notification.Name("com.local.avatarthu.menubar.showStatus")

func readObject(_ url: URL) -> Object {
    guard let data = try? Data(contentsOf: url),
          let value = try? JSONSerialization.jsonObject(with: data) as? Object else { return [:] }
    return value
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
    var pid: Int32 = 0
    var school = "尚未检查"
    var schoolOK = false
    var lastSuccess: Date?
    var pollSeconds: Double = 43200
    var nextScan: Date?
    var lark = false
    var actions = false
    var messages = false
    var busy = 0
    var queued = 0
    var awaiting = 0
    var attention = 0
    var reviews: [Object] = []
    var version = ""

    var kind: String {
        if !running { return "stopped" }
        if !schoolOK || attention > 0 || (lark && (!actions || !messages)) { return "attention" }
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
        s.version = health["version"] as? String ?? ""
        let session = config["session"] as? String ?? root.appendingPathComponent("session.json").path
        let keepalive = readObject(URL(fileURLWithPath: session).deletingLastPathComponent().appendingPathComponent("keepalive-status.json"))
        s.lastSuccess = dateValue(keepalive["last_success"])
        switch keepalive["state"] as? String {
        case "valid":
            s.schoolOK = s.lastSuccess.map { now.timeIntervalSince($0) < 15 * 60 } ?? false
            s.school = s.schoolOK ? "登录有效" : "保活记录待更新"
        case "expired": s.school = "登录已过期 · 请重新登录"
        case "unavailable": s.school = "暂时无法连接"
        default: break
        }
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
        let taskURLs = (try? FileManager.default.contentsOfDirectory(at: root.appendingPathComponent("data/tasks"), includingPropertiesForKeys: nil)) ?? []
        for url in taskURLs where url.pathExtension == "json" {
            let task = readObject(url)
            switch task["status"] as? String {
            case "working", "reviewing", "rewriting": s.busy += 1
            case "queued", "revision_ready", "delivery_pending": s.queued += 1
            case "awaiting": s.awaiting += 1; s.reviews.append(task)
            case "needs_student", "failed", "approval_invalid", "submission_unknown": s.attention += 1; s.reviews.append(task)
            default: break
            }
        }
        s.reviews.sort { ($0["updated_at"] as? String ?? "") > ($1["updated_at"] as? String ?? "") }
        return s
    }

    var diagnostic: Object {
        // Deliberately exclude account IDs, task text, document URLs and credentials.
        return ["state": kind, "daemon_running": running, "pid": pid, "school": school,
                "school_valid": schoolOK, "poll_interval_seconds": pollSeconds,
                "lark_enabled": lark, "actions_ready": actions, "messages_ready": messages,
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
        let symbol: String
        let color: NSColor
        switch snapshot.kind {
        case "stopped": symbol = "pause.circle"; color = .secondaryLabelColor
        case "attention": symbol = "exclamationmark.circle.fill"; color = .systemOrange
        case "busy": symbol = "arrow.triangle.2.circlepath.circle.fill"; color = .systemBlue
        default: symbol = "checkmark.circle.fill"; color = .systemGreen
        }
        let image = NSImage(systemSymbolName: symbol, accessibilityDescription: snapshot.headline)?
            .withSymbolConfiguration(NSImage.SymbolConfiguration(paletteColors: [color]))
        image?.isTemplate = false
        item.button?.image = image
        item.button?.imagePosition = .imageLeading
        item.button?.title = snapshot.awaiting > 0 ? " THU \(snapshot.awaiting)" : " THU"
        item.button?.toolTip = "AvatarTHU · \(snapshot.headline)\n网络学堂：\(snapshot.school)\n待审阅 \(snapshot.awaiting) · 需处理 \(snapshot.attention)"
        writeHealth("running")
    }

    func menuNeedsUpdate(_ menu: NSMenu) { refresh(); rebuild() }

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
        let heading = row("AvatarTHU")
        heading.attributedTitle = NSAttributedString(string: "AvatarTHU", attributes: [.font: NSFont.systemFont(ofSize: 15, weight: .semibold), .foregroundColor: NSColor.labelColor])
        row(snapshot.headline + (snapshot.running ? " · PID \(snapshot.pid)" : ""))
        row("网络学堂 · \(snapshot.school)", symbol: "graduationcap")
        row("最近保活 · \(timeText(snapshot.lastSuccess))（每 10 分钟）")
        let interval = snapshot.pollSeconds >= 3600 ? String(format: "%g 小时", snapshot.pollSeconds / 3600) : String(format: "%g 分钟", snapshot.pollSeconds / 60)
        row("课程扫描 · 每 \(interval)")
        let next = snapshot.nextScan.map { $0 <= Date() ? "已到期，等待调度" : timeText($0) } ?? "首次启动时扫描"
        row("下次扫描 · \(snapshot.running ? next : "恢复后台后检查")")
        if snapshot.lark {
            row("飞书 · 卡片\(snapshot.actions ? "已连接" : "未连接") / 消息\(snapshot.messages ? "已连接" : "未连接")", symbol: "bubble.left.and.bubble.right")
        } else { row("飞书 · 未启用（本地审阅）", symbol: "bubble.left") }
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
        row("刷新状态", action: #selector(refresh), symbol: "arrow.triangle.2.circlepath")
        menu.addItem(.separator())
        let quit = row("退出菜单栏（后台继续运行）", action: #selector(quitApp), symbol: "xmark.circle")
        quit.isEnabled = !busyAction
    }

    @objc func openCourses() { NSWorkspace.shared.open(root.appendingPathComponent("courses")) }
    @objc func openLog() { NSWorkspace.shared.open(root.appendingPathComponent("logs/daemon.log")) }
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
