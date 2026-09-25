# Third-party sources

AvatarTHU is an independent Go implementation. It does not invoke or bundle a Python interpreter, an AutoThu checkout, Selenium, ChromeDriver, or the `thulearn2018` package.

- `internal/app/school.go` and `internal/app/login.go` adapt network endpoints, cookie handling and macOS Chrome session import from [AutoThu](https://github.com/shiyt23/AutoThu), MIT, including our one-click login and keepalive contributions in [Sskift/AutoThu](https://github.com/Sskift/AutoThu/tree/feat/macos-one-click-login). Reference snapshot: `b5caba55ba2a53fcd1e08db1a5fab0d01231a39f`. The original copyright and license remain in [third_party/AutoThu-LICENSE](third_party/AutoThu-LICENSE) and distributed archives.
- Go's standard library and runtime are linked into the executable under Go's BSD-style license. End users do not install Go.
- [gofrs/flock](https://github.com/gofrs/flock): portable process locks, BSD-3-Clause.
- [gorilla/websocket](https://github.com/gorilla/websocket): the local browser DevTools connection, BSD-2-Clause.
- [ledongthuc/pdf](https://github.com/ledongthuc/pdf): PDF text extraction, BSD-3-Clause; based on rsc/pdf.
- [yuin/goldmark](https://github.com/yuin/goldmark): Markdown parsing, MIT.
- `golang.org/x/net`, `golang.org/x/sys`, `golang.org/x/image`: HTML, cookie domain validation, Windows process support, and image decoding, BSD-3-Clause.
- Feishu optionally invokes the official [Lark CLI](https://github.com/larksuite/cli). Its binary/source is not bundled. An existing installation is reused, or `@larksuite/cli@1.0.96` is installed through npm into the user's data directory when Feishu is enabled.
- Claude Code and Codex CLI remain separately installed tools with their own licenses, accounts and model settings. They are not bundled.
- The macOS menu bar's `TsinghuaSeal.svg` comes from [Tsinghua University Logo on Wikimedia Commons](https://commons.wikimedia.org/wiki/File:Tsinghua_University_Logo.svg), attributed there to Tsinghua University and marked public domain (PD-China and PD-1996). The unused SVG pattern was removed; AppKit renders the seal as a monochrome template. The university emblem remains Tsinghua University's mark; AvatarTHU is an independent project, not an official university application.
- Network API fields were also cross-checked against [thu-learn-lib](https://github.com/Harry-Chen/thu-learn-lib). No TypeScript implementation is bundled.
- Card 2.0 follows the official Lark CLI documentation. Historical layout inspiration: [Feishu-card-strong](https://github.com/TWe1v3/Feishu-card-strong); no third-party card skill is executed or bundled.

Pinned Go module versions are recorded in `go.mod` and `go.sum`. Release archives include the upstream dependency license files under `licenses/`.

Course materials, student data, sessions, credentials, and generated assignments are never part of the source repository or releases.
