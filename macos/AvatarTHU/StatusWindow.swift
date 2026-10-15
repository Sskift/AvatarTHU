import AppKit

enum DetailState {
    case information, working, success, failure
}

private let panelAccent = NSColor(name: "AvatarTHUPurple") { appearance in
    appearance.bestMatch(from: [.darkAqua, .aqua]) == .darkAqua
        ? NSColor(calibratedRed: 0.73, green: 0.62, blue: 0.89, alpha: 1)
        : NSColor(calibratedRed: 0.43, green: 0.29, blue: 0.63, alpha: 1)
}

private func panelLabel(_ text: String, size: CGFloat = 13, weight: NSFont.Weight = .regular, color: NSColor = .labelColor) -> NSTextField {
    let label = NSTextField(labelWithString: text)
    label.font = .systemFont(ofSize: size, weight: weight)
    label.textColor = color
    label.lineBreakMode = .byTruncatingTail
    return label
}

private func panelStack(_ views: [NSView], vertical: Bool = false, spacing: CGFloat = 12) -> NSStackView {
    let stack = NSStackView(views: views)
    stack.orientation = vertical ? .vertical : .horizontal
    stack.alignment = vertical ? .leading : .centerY
    stack.spacing = spacing
    return stack
}

private final class PanelCard: NSView {
    var tint: NSColor?
    init(_ content: NSView, padding: CGFloat = 16, tint: NSColor? = nil) {
        self.tint = tint
        super.init(frame: .zero)
        wantsLayer = true
        layer?.cornerRadius = 14
        layer?.borderWidth = 0.5
        addSubview(content)
        content.translatesAutoresizingMaskIntoConstraints = false
        NSLayoutConstraint.activate([
            content.leadingAnchor.constraint(equalTo: leadingAnchor, constant: padding),
            content.trailingAnchor.constraint(equalTo: trailingAnchor, constant: -padding),
            content.topAnchor.constraint(equalTo: topAnchor, constant: padding),
            content.bottomAnchor.constraint(equalTo: bottomAnchor, constant: -padding)
        ])
    }
    required init?(coder: NSCoder) { fatalError("init(coder:) has not been implemented") }
    override var wantsUpdateLayer: Bool { true }
    override func updateLayer() {
        layer?.backgroundColor = (tint?.withAlphaComponent(0.08) ?? NSColor.controlBackgroundColor).cgColor
        layer?.borderColor = NSColor.separatorColor.withAlphaComponent(0.3).cgColor
    }
    override func viewDidChangeEffectiveAppearance() { super.viewDidChangeEffectiveAppearance(); needsDisplay = true }
}

private final class ServiceCard: NSView {
    let status = panelLabel("尚未检查", size: 12, color: .secondaryLabelColor)
    let dot = NSImageView()
    init(_ title: String, symbol: String) {
        super.init(frame: .zero)
        let icon = NSImageView(image: NSImage(systemSymbolName: symbol, accessibilityDescription: nil) ?? NSImage())
        icon.contentTintColor = panelAccent
        icon.widthAnchor.constraint(equalToConstant: 22).isActive = true
        icon.heightAnchor.constraint(equalToConstant: 22).isActive = true
        dot.image = NSImage(systemSymbolName: "circle.fill", accessibilityDescription: nil)
        dot.widthAnchor.constraint(equalToConstant: 7).isActive = true
        dot.heightAnchor.constraint(equalToConstant: 7).isActive = true
        let labels = panelStack([panelLabel(title, size: 13, weight: .medium), panelStack([dot, status], spacing: 6)], vertical: true, spacing: 6)
        labels.setContentHuggingPriority(.defaultLow, for: .horizontal)
        status.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
        let card = PanelCard(panelStack([icon, labels], spacing: 14))
        addSubview(card)
        card.translatesAutoresizingMaskIntoConstraints = false
        NSLayoutConstraint.activate([
            card.leadingAnchor.constraint(equalTo: leadingAnchor), card.trailingAnchor.constraint(equalTo: trailingAnchor),
            card.topAnchor.constraint(equalTo: topAnchor), card.bottomAnchor.constraint(equalTo: bottomAnchor)
        ])
    }
    required init?(coder: NSCoder) { fatalError("init(coder:) has not been implemented") }
    func update(_ indicator: Indicator) {
        status.stringValue = indicator.text
        dot.contentTintColor = indicator.color
        toolTip = indicator.detail
        setAccessibilityLabel(status.stringValue)
    }
}

private final class PanelDocument: NSView {
    override var isFlipped: Bool { true }
}

final class StatusWindowController: NSWindowController {
    private let heading = panelLabel("运行概览", size: 25, weight: .semibold)
    private let subtitle = panelLabel("正在读取状态…", size: 12, color: .secondaryLabelColor)
    private let badge = panelLabel("读取中", size: 11, weight: .medium)
    private let badgeDot = NSImageView()
    private let spinner = NSProgressIndicator()
    private let overview = NSStackView()
    private let message = NSTextView()
    private let output = NSTextView()
    private let details = NSStackView()
    private let disclosure = NSButton()
    private let copyButton = NSButton()
    private var messageCard: NSView!
    private var outputCard: NSView!
    private var actionButtons: [NSButton] = []
    private var metrics: [NSTextField] = []
    private let school = ServiceCard("网络学堂", symbol: "graduationcap")
    private let lark = ServiceCard("飞书", symbol: "bubble.left.and.bubble.right")
    private let claude = ServiceCard("Claude", symbol: "sparkle")
    private let codex = ServiceCard("Codex", symbol: "terminal")
    private let lastKeepalive = panelLabel("尚无记录", size: 13, weight: .medium)
    private let nextScan = panelLabel("等待调度", size: 13, weight: .medium)
    private let scanInterval = panelLabel("每 12 小时扫描课程", size: 11, color: .secondaryLabelColor)
    private var isOverview = true
    private var state: DetailState = .information
    private var expanded = false
    private var rawText = ""

    init(owner: MenuBar) {
        let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 740, height: 650), styleMask: [.titled, .closable, .miniaturizable, .resizable], backing: .buffered, defer: false)
        window.isReleasedWhenClosed = false
        window.titlebarAppearsTransparent = true
        window.titleVisibility = .hidden
        window.backgroundColor = .windowBackgroundColor
        window.minSize = NSSize(width: 640, height: 560)
        window.isMovableByWindowBackground = true
        super.init(window: window)
        build(owner: owner)
        window.center()
    }
    required init?(coder: NSCoder) { fatalError("init(coder:) has not been implemented") }

    private func build(owner: MenuBar) {
        let scroll = NSScrollView()
        scroll.drawsBackground = false
        scroll.hasVerticalScroller = true
        scroll.autohidesScrollers = true
        let document = PanelDocument()
        scroll.documentView = document
        document.translatesAutoresizingMaskIntoConstraints = false
        document.widthAnchor.constraint(equalTo: scroll.contentView.widthAnchor).isActive = true
        let content = panelStack([], vertical: true, spacing: 18)
        document.addSubview(content)
        content.translatesAutoresizingMaskIntoConstraints = false
        NSLayoutConstraint.activate([
            content.leadingAnchor.constraint(equalTo: document.leadingAnchor, constant: 24),
            content.trailingAnchor.constraint(equalTo: document.trailingAnchor, constant: -24),
            content.topAnchor.constraint(equalTo: document.topAnchor, constant: 18),
            content.bottomAnchor.constraint(equalTo: document.bottomAnchor, constant: -20)
        ])
        func section(_ view: NSView) {
            content.addArrangedSubview(view)
            view.widthAnchor.constraint(equalTo: content.widthAnchor).isActive = true
        }

        let logo = NSImageView(image: sealImage() ?? NSImage(systemSymbolName: "graduationcap.fill", accessibilityDescription: nil)!)
        logo.contentTintColor = panelAccent
        logo.imageScaling = .scaleProportionallyUpOrDown
        logo.widthAnchor.constraint(equalToConstant: 32).isActive = true
        logo.heightAnchor.constraint(equalToConstant: 32).isActive = true
        let mark = PanelCard(logo, padding: 12, tint: panelAccent)
        let title = panelStack([panelLabel("AvatarTHU", size: 12, weight: .semibold, color: panelAccent), heading, subtitle], vertical: true, spacing: 5)
        title.setContentHuggingPriority(.defaultLow, for: .horizontal)
        heading.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
        subtitle.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
        badgeDot.image = NSImage(systemSymbolName: "circle.fill", accessibilityDescription: nil)
        badgeDot.widthAnchor.constraint(equalToConstant: 7).isActive = true
        badgeDot.heightAnchor.constraint(equalToConstant: 7).isActive = true
        spinner.style = .spinning
        spinner.controlSize = .small
        spinner.isDisplayedWhenStopped = false
        let badgeView = PanelCard(panelStack([badgeDot, badge, spinner], spacing: 6), padding: 9)
        badgeView.widthAnchor.constraint(equalToConstant: 104).isActive = true
        let headerSpacer = NSView()
        headerSpacer.setContentHuggingPriority(NSLayoutConstraint.Priority(1), for: .horizontal)
        section(panelStack([mark, title, headerSpacer, badgeView], spacing: 16))

        overview.orientation = .vertical
        overview.alignment = .leading
        overview.spacing = 16
        func overviewRow(_ view: NSView) {
            overview.addArrangedSubview(view)
            view.widthAnchor.constraint(equalTo: overview.widthAnchor).isActive = true
        }
        let counters = panelStack([], spacing: 0)
        counters.distribution = .fillEqually
        for (index, name) in ["处理中", "排队中", "待审阅", "需处理"].enumerated() {
            let count = panelLabel("0", size: 28, weight: .semibold, color: index == 2 ? panelAccent : .labelColor)
            metrics.append(count)
            counters.addArrangedSubview(panelStack([count, panelLabel(name, size: 11, color: .secondaryLabelColor)], vertical: true, spacing: 4))
        }
        overviewRow(PanelCard(counters, padding: 18))
        overviewRow(panelLabel("连接与服务", size: 12, weight: .semibold, color: .secondaryLabelColor))
        for pair in [[school, lark], [claude, codex]] {
            let row = panelStack(pair, spacing: 12)
            row.distribution = .fillEqually
            overviewRow(row)
        }
        let keepalive = panelStack([panelLabel("最近保活", size: 11, color: .secondaryLabelColor), lastKeepalive, panelLabel("每 10 分钟自动检查", size: 11, color: .tertiaryLabelColor)], vertical: true, spacing: 6)
        let schedule = panelStack([panelLabel("下次课程扫描", size: 11, color: .secondaryLabelColor), nextScan, scanInterval], vertical: true, spacing: 6)
        let timing = panelStack([keepalive, schedule], spacing: 20)
        timing.distribution = .fillEqually
        overviewRow(PanelCard(timing, padding: 18))
        section(overview)

        messageCard = textCard(message, monospaced: false, height: 280)
        section(messageCard)
        outputCard = textCard(output, monospaced: true, height: 210)
        disclosure.title = "详细信息"
        disclosure.image = NSImage(systemSymbolName: "chevron.right", accessibilityDescription: nil)
        disclosure.imagePosition = .imageLeading
        disclosure.isBordered = false
        disclosure.font = .systemFont(ofSize: 12)
        disclosure.contentTintColor = .secondaryLabelColor
        disclosure.target = self
        disclosure.action = #selector(toggleDetails)
        copyButton.title = "复制详情"
        copyButton.bezelStyle = .inline
        copyButton.font = .systemFont(ofSize: 11)
        copyButton.target = self
        copyButton.action = #selector(copyDetails)
        let spacer = NSView()
        spacer.setContentHuggingPriority(.defaultLow, for: .horizontal)
        details.orientation = .vertical
        details.alignment = .leading
        details.spacing = 10
        let detailsBar = panelStack([disclosure, spacer, copyButton])
        for view in [detailsBar, outputCard!] {
            details.addArrangedSubview(view)
            view.widthAnchor.constraint(equalTo: details.widthAnchor).isActive = true
        }
        section(details)

        let workspace = NSButton(title: "打开工作台", target: owner, action: #selector(MenuBar.openWorkspace))
        workspace.bezelStyle = .rounded
        workspace.bezelColor = panelAccent
        workspace.contentTintColor = panelAccent
        workspace.image = NSImage(systemSymbolName: "rectangle.3.group", accessibilityDescription: nil)
        workspace.imagePosition = .imageLeading
        let refresh = NSButton(title: "刷新状态", target: owner, action: #selector(MenuBar.showStatus))
        refresh.bezelStyle = .rounded
        refresh.image = NSImage(systemSymbolName: "arrow.clockwise", accessibilityDescription: nil)
        refresh.imagePosition = .imageLeading
        let menu = NSButton(title: "更多操作", target: owner, action: #selector(MenuBar.openStatusMenu))
        menu.bezelStyle = .rounded
        actionButtons = [workspace, refresh]
        let footerSpacer = NSView()
        footerSpacer.setContentHuggingPriority(.defaultLow, for: .horizontal)
        let footer = panelStack([menu, footerSpacer, refresh, workspace], spacing: 10)
        let divider = NSBox()
        divider.boxType = .separator
        let root = window!.contentView!
        for view in [scroll, divider, footer] { root.addSubview(view); view.translatesAutoresizingMaskIntoConstraints = false }
        NSLayoutConstraint.activate([
            scroll.topAnchor.constraint(equalTo: root.topAnchor), scroll.leadingAnchor.constraint(equalTo: root.leadingAnchor),
            scroll.trailingAnchor.constraint(equalTo: root.trailingAnchor), scroll.bottomAnchor.constraint(equalTo: divider.topAnchor, constant: -12),
            divider.leadingAnchor.constraint(equalTo: root.leadingAnchor, constant: 24), divider.trailingAnchor.constraint(equalTo: root.trailingAnchor, constant: -24),
            divider.bottomAnchor.constraint(equalTo: footer.topAnchor, constant: -16),
            footer.leadingAnchor.constraint(equalTo: root.leadingAnchor, constant: 24), footer.trailingAnchor.constraint(equalTo: root.trailingAnchor, constant: -24),
            footer.bottomAnchor.constraint(equalTo: root.bottomAnchor, constant: -20)
        ])
    }

    private func textCard(_ text: NSTextView, monospaced: Bool, height: CGFloat) -> NSView {
        text.isEditable = false
        text.isSelectable = true
        text.drawsBackground = false
        text.textColor = .labelColor
        text.font = monospaced ? .monospacedSystemFont(ofSize: 11, weight: .regular) : .systemFont(ofSize: 13)
        text.textContainerInset = NSSize(width: 16, height: 14)
        text.autoresizingMask = [.width]
        text.textContainer?.widthTracksTextView = true
        let paragraph = NSMutableParagraphStyle()
        paragraph.lineSpacing = monospaced ? 3 : 5
        text.defaultParagraphStyle = paragraph
        let scroll = NSScrollView()
        scroll.drawsBackground = false
        scroll.hasVerticalScroller = true
        scroll.autohidesScrollers = true
        scroll.documentView = text
        scroll.heightAnchor.constraint(equalToConstant: height).isActive = true
        return PanelCard(scroll, padding: 0)
    }

    func show(title: String, text: String, snapshot: Snapshot, overview: Bool, state: DetailState) {
        if isOverview != overview { expanded = false }
        if overview && state == .failure { expanded = true }
        self.isOverview = overview
        self.state = state
        rawText = text
        window?.title = title
        self.overview.isHidden = !overview
        messageCard.isHidden = overview
        details.isHidden = !overview
        message.string = text
        output.string = text
        copyButton.isHidden = !expanded
        outputCard.isHidden = !expanded
        disclosure.image = NSImage(systemSymbolName: expanded ? "chevron.down" : "chevron.right", accessibilityDescription: nil)
        spinner.isHidden = state != .working
        if state == .working { spinner.startAnimation(nil) } else { spinner.stopAnimation(nil) }
        actionButtons.forEach { $0.isEnabled = state != .working }
        if overview { update(snapshot: snapshot) }
        else {
            heading.stringValue = title
            subtitle.stringValue = state == .working ? "操作正在进行，完成后会自动更新。" : ""
            setBadge(state == .working ? "执行中" : state == .failure ? "未完成" : state == .success ? "已完成" : "详情", color: state == .failure ? .systemRed : state == .success ? .systemGreen : panelAccent)
        }
        window?.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
    }

    func update(snapshot: Snapshot) {
        guard isOverview else { return }
        heading.stringValue = snapshot.headline
        subtitle.stringValue = snapshot.modeText
        let values = [snapshot.busy, snapshot.queued, snapshot.awaiting, snapshot.attention]
        for (label, value) in zip(metrics, values) { label.stringValue = String(value) }
        metrics[3].textColor = snapshot.attention > 0 ? .systemOrange : .labelColor
        school.update(snapshot.learn); lark.update(snapshot.larkCLI)
        claude.update(snapshot.claude); codex.update(snapshot.codex)
        lastKeepalive.stringValue = time(snapshot.lastSuccess)
        nextScan.stringValue = !snapshot.running ? "启动后台后检查" : snapshot.nextScan.map { $0 <= Date() ? "已到期，等待调度" : time($0) } ?? "首次启动时扫描"
        let interval = snapshot.pollSeconds >= 3600 ? String(format: "%g 小时", snapshot.pollSeconds / 3600) : String(format: "%g 分钟", snapshot.pollSeconds / 60)
        scanInterval.stringValue = "每 \(interval)扫描课程"
        if state == .working { setBadge("更新中", color: panelAccent) }
        else if state == .failure { setBadge("读取失败", color: .systemRed) }
        else {
            let labels = ["running": "运行中", "busy": "处理中", "attention": "需关注", "stopped": "已停止"]
            setBadge(labels[snapshot.kind] ?? "运行中", color: snapshot.kind == "attention" ? .systemOrange : snapshot.kind == "stopped" ? .secondaryLabelColor : .systemGreen)
        }
    }

    private func setBadge(_ text: String, color: NSColor) {
        badge.stringValue = text
        badgeDot.contentTintColor = color
    }
    private func time(_ date: Date?) -> String {
        guard let date = date else { return "尚无记录" }
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "zh_CN")
        formatter.dateFormat = Calendar.current.isDateInToday(date) ? "'今天' HH:mm" : "MM-dd HH:mm"
        return formatter.string(from: date)
    }
    @objc private func toggleDetails() {
        expanded.toggle()
        outputCard.isHidden = !expanded
        copyButton.isHidden = !expanded
        disclosure.image = NSImage(systemSymbolName: expanded ? "chevron.down" : "chevron.right", accessibilityDescription: nil)
        if expanded {
            window?.contentView?.layoutSubtreeIfNeeded()
            outputCard.scrollToVisible(outputCard.bounds)
        }
    }
    @objc private func copyDetails() {
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(rawText, forType: .string)
    }
}
