import AppKit

func runMenuBarTests() throws {
    func require(_ condition: @autoclosure () -> Bool, _ message: String) throws {
        if !condition() { throw NSError(domain: "MenuBarTests", code: 1, userInfo: [NSLocalizedDescriptionKey: message]) }
    }
    func waitUntil(_ condition: () -> Bool) throws {
        let deadline = Date().addingTimeInterval(5)
        while !condition(), Date() < deadline {
            RunLoop.main.run(until: Date().addingTimeInterval(0.01))
        }
        try require(condition(), "Timed out waiting for the menu to update")
    }

    let app = NSApplication.shared
    app.setActivationPolicy(.accessory)
    let release = DispatchSemaphore(value: 0)
    let enteredLoad = DispatchSemaphore(value: 0)
    var loads = 0
    let monitor = MenuBar(loadSnapshot: { root in
        loads += 1
        if loads == 2 {
            enteredLoad.signal()
            // Keep a state refresh pending until the UI check completes. The
            // timeout makes a synchronous regression fail without hanging.
            _ = release.wait(timeout: .now() + 1.5)
        }
        return Snapshot.load(root: root)
    })
    monitor.item = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
    defer { NSStatusBar.system.removeStatusItem(monitor.item) }
    monitor.menu.delegate = monitor
    monitor.menu.autoenablesItems = false
    monitor.lastCLIProbe = Date() // No real CLI probes or account access.
    let config = monitor.root.appendingPathComponent("config.json")
    let tasks = monitor.root.appendingPathComponent("data/tasks")
    try FileManager.default.createDirectory(at: tasks, withIntermediateDirectories: true)
    try Data("{}".utf8).write(to: config)
    try Data("{\"status\":\"awaiting\"}".utf8).write(to: tasks.appendingPathComponent("first.json"))
    monitor.refresh()
    try waitUntil { monitor.snapshot.awaiting == 1 }
    monitor.rebuild()
    let cachedItems = monitor.menu.items.map { ObjectIdentifier($0) }

    try Data("{\"status\":\"awaiting\"}".utf8).write(to: tasks.appendingPathComponent("second.json"))
    monitor.menu.delegate?.menuWillOpen?(monitor.menu)
    let start = ProcessInfo.processInfo.systemUptime
    monitor.menuNeedsUpdate(monitor.menu)
    let openSeconds = ProcessInfo.processInfo.systemUptime - start
    try require(openSeconds < 0.5, "Menu opening waited for state-file I/O: \(openSeconds) s")
    try require(enteredLoad.wait(timeout: .now() + 1) == .success, "Snapshot loading did not start")
    for _ in 0..<20 { monitor.menuNeedsUpdate(monitor.menu) }
    var mainQueueResponded = false
    DispatchQueue.main.async { mainQueueResponded = true }
    try waitUntil { mainQueueResponded }
    try require(monitor.snapshot.awaiting == 1, "Slow read did not keep the previous snapshot available")
    try require(monitor.menu.items.map { ObjectIdentifier($0) } == cachedItems, "Opening rebuilt the cached menu")

    release.signal()
    try waitUntil { monitor.snapshot.awaiting == 2 }
    try require(monitor.menu.items.map { ObjectIdentifier($0) } == cachedItems, "Refresh replaced rows while the menu was open")
    monitor.menu.delegate?.menuDidClose?(monitor.menu)
    try waitUntil { monitor.menu.items.contains { $0.title.contains("2 待审阅") } }

    // A failed background CLI launch must release the probe and leave the UI usable.
    monitor.checkCLIsIfNeeded(force: true)
    try waitUntil { monitor.cliProbe == nil && monitor.cliProbeError != nil }
    print(String(format: "PASS: blocked state read; menu callback %.3f ms; main queue responsive; open rows preserved; closed menu updated; launch failure recovered", openSeconds * 1000))
}
