# AvatarTHU

把网络学堂的课程资料、作业处理和本人审阅串起来的本地课程助手。

**现在使用 Go 原生程序。** 每个平台一个可执行文件，不需要安装 Python、pip、虚拟环境、Go 或 Java。空闲时只保留调度进程；有作业时才启动主写和复审 CLI。飞书是可选功能，不启用也可以下载材料、完成作业和本地审阅。

## 安装与启动

从 [Releases](https://github.com/Sskift/AvatarTHU/releases) 下载对应系统与架构的文件，解压后执行一次初始化。压缩包只有原生程序、说明与许可证，没有解释器环境。

macOS / Linux：

```sh
chmod +x avatarthu
./avatarthu init
```

Windows PowerShell：

```powershell
.\avatarthu.exe init
```

初始化把程序安装到用户目录并配置命令入口，随后在新终端中直接使用 `avatarthu`。Windows 若新终端尚未刷新用户 PATH，可以重新登录系统；安装命令打印的完整路径也可以直接使用。macOS 首次打开下载程序时若被系统拦截，可在“系统设置 → 隐私与安全性”允许打开。当前预发布二进制尚未做 Apple 公证或 Windows 代码签名。

初始化默认打开网络学堂登录入口并启动后台。也可分开执行：

```sh
avatarthu init --no-login --no-start
avatarthu tools
avatarthu login thu
avatarthu doctor
avatarthu service start
```

`login thu` 在 macOS 优先复用 Chrome 中已有的网络学堂登录；不可用时打开 AvatarTHU 自己的浏览器窗口，由本人完成 SSO / 双因素认证。Windows 使用 Chrome 或 Edge。不依赖 Selenium、ChromeDriver 或外部 AutoThu 程序。

## 必需和可选工具

| 功能 | 外部工具 |
| --- | --- |
| 调度、同步、下载、目录管理、审阅页 | 无额外语言运行时 |
| 网络学堂首次登录 / 重新认证 | 已安装的 Chrome；Windows 也支持 Edge |
| 主写与独立复审 | Claude Code、Codex CLI，都需安装并登录 |
| 飞书文档、卡片、批注与回调 | 可选 Lark CLI；安装时复用现有版本，缺失时可用 npm 自动安装 |
| 特定作业的编译、实验或报告工具 | 由题目决定；无法完成或验证的部分会明确报告 |

运行 `avatarthu tools` 可查看安装方式，`avatarthu doctor` 检查两个执行器的路径、版本、登录及所需参数。找不到程序、未登录、额度限制、网络问题、权限不足、版本不兼容和默认配置不可用会分别给出说明。

如果已有 Node.js/npm，可安装两种 CLI：

```sh
npm install -g @anthropic-ai/claude-code @openai/codex
claude
codex
avatarthu doctor
```

也可以使用 [Claude Code 原生安装](https://code.claude.com/docs/en/setup) 和 [Codex 官方安装方式](https://github.com/openai/codex#quickstart)。AvatarTHU 不强制安装 Node.js；只有选择 npm 安装外部 CLI 时才需要。

## 一个进程，两种频率

```sh
avatarthu configure --poll-interval 12h
avatarthu service start
avatarthu status
avatarthu keepalive status
```

1. **网络学堂保活固定每 10 分钟。** 验证课程接口，保存服务端更新的 Cookie；可复用本项目浏览器配置中的有效 SSO 会话。需要重新认证时明确提示 `avatarthu login thu`。
2. **课程扫描默认每 12 小时。** 支持 `30m`、`6h`、`12h`、`1d` 等设置，最低一分钟。同步失败后 15 分钟重试。
3. **本人修改意见每分钟检查。** 不需要等到下一轮课程扫描；作业运行期间，同一个 Go 进程继续保活。
4. **休眠期间无法运行。** 电脑唤醒后按实际时间补查；macOS 由 LaunchAgent、Windows 由当前用户任务计划程序负责启动与恢复。Windows 不需要管理员服务或保存系统密码；需保持用户已登录。Linux 提供 systemd 用户服务。

`service start` 不重复启动已运行的进程。`service stop` 停止调度、保活和飞书监听；保留所有数据。`avatarthu daemon` 可以直接在前台运行。

## 两种交叉复审模式

```sh
# Claude 主写，Codex 独立复审
avatarthu configure --mode claude-codex

# Codex 主写，Claude 独立复审
avatarthu configure --mode codex-claude

# 每版最多自动复审 3 轮；0 表示不限
avatarthu configure --max-review-rounds 3
```

每个阶段使用新进程、新会话、新工作目录；两边分别使用自己的默认模型配置。AvatarTHU 不指定、比较或约束模型。

复审者只收到原始作业、课件和当前候选文件，不传主写对话、自查记录、之前的修改意见或复审结论，并关闭该次会话的记忆、额外规则发现和外部工具集成。它独立读取题目、检查答案和产物，必要时自行复算或运行。这里实现的是输入与会话隔离；本机 CLI 仍受其自身权限与执行环境约束。

复审不通过时，把具体评论交给主写，在新会话中修改后再复审。每轮完成的评论和检查记录都持久保存，即使中途退出也不会丢掉已完成的复审。达到轮数上限仍未通过时交给本人处理，不呈现为可直接提交。

交叉复审能发现遗漏和错误，但不等于数学、实验或程序正确性的保证。最终审阅和提交决定始终由本人作出。

## 飞书与审阅

```sh
avatarthu login lark
avatarthu notifications off
avatarthu notifications on
```

飞书登录会复用本地 Lark CLI 的有效登录态；没有配置时引导创建应用与授权。未安装 Lark CLI 时，`login lark` 可通过本机 npm 安装到 AvatarTHU 的工具目录。不同飞书账号不能直接接管已有作业的确认权限。

1. 未读公告先归档、发给本人，**成功送达回执落盘后才标已读**。标记失败只重试标记，避免重复发消息。不开飞书时保持未读。
2. 有待交作业时下载题目附件、描述和课件，在该作业目录内完成主写与复审。
3. 会话里只发审阅卡片，全部产物嵌在对应云文档中。文档固定五部分：**作业描述、完成情况与关键结果、完整产物、审阅与操作、历次独立复审**。支持有序列表、公式、表格和实际结果图；报告通过原生 PDF / Office 附件预览，保留原始排版。
4. 可以在文档批注后点击“按文档批注修改”，也可以直接在卡片写意见、回复当前卡片，或在本地使用 `avatarthu revise 作业编号 --feedback "修改意见"`。
5. 只有本人当前版本卡片上的提交操作才会上传。版本、卡片消息、随机凭据和本地文件哈希必须匹配，并再次核对学校端要求、附件和截止时间。修改后旧卡片失效。
6. 提交状态先落盘再请求学校接口。超时、断连等导致结果未知时，**不自动重试上传**，由本人去网络学堂核对。

纯本地模式会生成同样分节的 HTML 审阅页，用 `avatarthu status` 查看路径；提交需本人在学校网页操作。启用飞书后可把现有版本发布为云文档与卡片，无需重做作业。

## 文件在哪里

统一入口为 `~/.avatarthu`，Windows 对应 `%USERPROFILE%\.avatarthu`。macOS 为保留历史数据，实际目录继续使用 `~/.local/share/avatarthu`，入口使用符号链接，不复制已有作业。

```text
~/.avatarthu/
├── bin/avatarthu                  # 原生可执行文件
├── config.json                   # 分工、扫描频率、CLI 路径
├── session.json                  # 本人网络学堂会话
├── browser-profile/              # 本项目的浏览器登录配置
├── courses/
│   └── 学期/课程名--课程标识/
│       ├── course.json
│       ├── notices/              # 公告原文和元数据
│       ├── courseware/           # 增量下载课件
│       └── homework/作业名--作业标识/
│           ├── source/           # 原题、说明和附件
│           ├── runs/r1/round-1/  # 主写和独立复审的不同目录
│           ├── outputs/r1/       # 冻结的提交文件与完整产物
│           ├── reviews/r1/       # 本地审阅、云文档草稿和回执
│           └── state.json
├── data/                         # 全局任务索引、调度与送达回执
├── logs/                         # 后台和执行错误日志
└── tools/                        # 可选安装的 Lark CLI
```

旧版本的 `outbox/`、`data/reviews/` 与已发送卡片继续兼容，不搬动已冻结的文件。可通过 `AVATARTHU_HOME` 指定独立数据目录。课程、会话、账号、日志和生成作业均不进入 Git。

## 常用命令

```sh
avatarthu run                     # 立即扫描并处理
avatarthu run --sync-only         # 只同步、下载及推送公告
avatarthu run --task 作业编号      # 同步后只处理指定作业
avatarthu keepalive run           # 立即保活并尝试恢复登录
avatarthu status                  # 服务、登录、作业和审阅入口
avatarthu service stop            # 停止后台，保留数据
avatarthu uninstall               # 停用服务，仍保留数据
```

## 开发与分发

仅开发者需要 Go 1.27.1。生产程序使用 `CGO_ENABLED=0` 构建，不需要额外动态库或解释器。

```sh
go test ./...
go vet ./...
CGO_ENABLED=0 go build -trimpath -o build/avatarthu ./cmd/avatarthu
go run ./cmd/release --os windows --arch amd64 --out dist
```

CI 在 macOS、Windows 和 Linux 上检查核心工作流与全新安装。发布支持 macOS arm64 / amd64、Windows amd64 / arm64、Linux amd64 / arm64，附带 SHA256SUMS 与第三方许可证。

当前为 **Alpha**，适合愿意自行审阅作业、反馈问题的使用者试用。需要长期跨账号验证学校端变化、不同课程材料与各平台浏览器认证。仓库是否可见取决于 GitHub 的访问权限；私有仓库的 Release 也需要相应权限。

网络学堂相关接口和 macOS 会话导入改写自 AutoThu 及我们的登录、保活贡献，已经内置在本项目中。来源与许可证见 [THIRD_PARTY.md](THIRD_PARTY.md)。
