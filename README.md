# AvatarTHU

**简体中文** · [English](README.en.md)

把网络学堂的课程资料、作业处理和本人审阅串起来的本地课程助手。

**现在使用 Go 原生程序。** 每个平台一个可执行文件，不需要安装 Python、pip、虚拟环境、Go 或 Java。空闲时只保留调度进程；有作业时才启动主写和复审 CLI。飞书是可选功能，不启用也可以下载材料、完成作业和本地审阅。

[下载最新版本](https://github.com/Sskift/AvatarTHU/releases) · [反馈问题](https://github.com/Sskift/AvatarTHU/issues) · [来源与许可证](THIRD_PARTY.md)

[从零开始安装](#quickstart) · [复制给 Agent 自动配置](#agent-setup)

## 使用效果

下图使用**虚构的演示作业与复审记录**，不代表真实作业完成或模型复审结果。卡片是程序生成的 JSON 的本地渲染预览，云文档是实际飞书正文截图。截图仅保留产品内容，不包含姓名、头像、学号、账号、私人链接或真实课程资料。详见[截图说明](docs/images/README.md)。

### 1. 在卡片里决定下一步

查看当前版本与复审状态，打开完整文档，填写修改意见；由本人决定是否提交。

<img src="docs/images/review-card.png" alt="飞书审阅卡片：当前版本、独立复审状态、文档入口、按批注修改和本人确认提交" width="720">

### 2. 在云文档里对照题目和结果

作业描述、编号要求和结果表分节呈现，先看需要做什么，再看这一版做出了什么。

![云文档中的作业描述与关键结果](docs/images/review-document.png)

### 3. 集中查看文件和历次复审

产物使用原生附件，可以直接预览或下载；文档末尾保留每轮复审的结论、具体评论和待核对项。

<details>
<summary>查看完整产物截图</summary>

![云文档中的完整产物与原生附件](docs/images/review-files.png)

</details>

<details>
<summary>查看历次独立复审截图</summary>

![云文档中的独立复审记录](docs/images/review-history.png)

</details>

<a id="quickstart"></a>

## 从零开始：第一次运行

首次使用按下面六步完成。**不需要克隆仓库或安装 Go、Python。** 需要本人的网络学堂账号，以及已安装并登录的 Claude Code 和 Codex CLI；首次网络学堂登录需要 Chrome，Windows 也支持 Edge。飞书可选，不影响本地工作流。也可以直接使用下方的 [Agent 配置提示词](#agent-setup)。

### 1. 下载适合自己电脑的程序

打开 [Releases](https://github.com/Sskift/AvatarTHU/releases)，选择最新可用版本并展开 Assets；目前提供 Alpha 预发布版本。

| 电脑 | 下载文件 |
| --- | --- |
| macOS · Apple 芯片 | `avatarthu-darwin-arm64.tar.gz` |
| macOS · Intel | `avatarthu-darwin-amd64.tar.gz` |
| Windows · Intel / AMD 64 位 | `avatarthu-windows-amd64.zip` |
| Windows · ARM64 | `avatarthu-windows-arm64.zip` |
| Linux · x86_64 | `avatarthu-linux-amd64.tar.gz` |
| Linux · ARM64 / aarch64 | `avatarthu-linux-arm64.tar.gz` |

macOS / Linux 可用 `uname -m` 查看架构；Windows 可在“设置 → 系统 → 系统信息”查看系统类型。下载同一版本的 `SHA256SUMS`，用 `shasum -a 256 文件名`（macOS）、`sha256sum 文件名`（Linux）或 `Get-FileHash 文件名 -Algorithm SHA256`（PowerShell）与其中对应文件的值核对，然后解压。

### 2. 安装命令，暂不启动后台

在解压目录打开终端。macOS / Linux：

```sh
chmod +x avatarthu
./avatarthu init --no-login --no-start
```

Windows PowerShell：

```powershell
.\avatarthu.exe init --no-login --no-start
```

初始化把程序安装到用户目录并配置命令入口，随后在新终端中直接使用 `avatarthu`。Windows 若新终端尚未刷新用户 PATH，可以重新登录系统；安装命令打印的完整路径也可以直接使用。macOS 首次打开下载程序时若被系统拦截，可在“系统设置 → 隐私与安全性”允许打开。当前预发布二进制尚未做 Apple 公证或 Windows 代码签名。

新开终端后检查：

```sh
avatarthu --version
```

这一步只安装和初始化。直接运行不带参数的 `init` 会登录并启动后台，适合工具与配置已就绪的使用者。

### 3. 准备主写和复审工具

两种 CLI 都需要安装和登录，即使你正在其中一个 Agent 里完成配置。已有可用安装可直接复用；缺少时运行 `avatarthu tools` 查看安装方法，也可参照后面的[工具说明](#必需和可选工具)。分别运行 `claude`、`codex` 完成登录后退出交互会话，再检查：

```sh
avatarthu tools
avatarthu doctor
```

预期两个 CLI 都显示“可启动，登录检查通过”。这不代表已验证实时额度或完成了作业；实际写作与复审会使用你自己的 CLI 账号和额度。工具未就绪时仍可只同步材料，先不执行第 6 步。

### 4. 选择分工，登录网络学堂

```sh
avatarthu configure --mode claude-codex --poll-interval 12h --max-review-rounds 3
avatarthu login thu
avatarthu keepalive run
```

这里选择 Claude 主写、Codex 复审；交换分工可把 `claude-codex` 改为 `codex-claude`。`12h` 可换成 `30m`、`6h` 或 `1d`；保活固定每 10 分钟。两边沿用各自默认模型。

`login thu` 在 macOS 优先复用 Chrome 中已有的网络学堂登录；不可用时打开 AvatarTHU 自己的浏览器窗口，由本人完成 SSO / 双因素认证。Windows 使用 Chrome 或 Edge。不依赖 Selenium、ChromeDriver 或外部 AutoThu 程序。

`keepalive run` 返回的 `state` 应为 `valid`，表示学校会话可用。

### 5. 选择是否接入飞书，完成首次同步

**只在本地使用：** 全新安装默认关闭飞书，直接执行下方同步命令即可。已有配置需要关闭飞书时，运行 `avatarthu notifications off`。

**需要飞书卡片和云文档：** 先执行以下命令，按引导完成应用配置和本人授权。缺少 Lark CLI 时可通过本机 npm 安装；未准备 Node.js/npm 的使用者也可先使用纯本地模式。

```sh
avatarthu login lark --no-start
```

此处的 `--no-start` 用于先完成配置；不带它会直接启动后台。随后执行首次同步：

```sh
avatarthu run --sync-only
avatarthu status
```

预期显示“同步完成”，课程材料出现在 `~/.avatarthu/courses/`，发现的待交作业显示为 `queued`。没有待交作业时列表为空也是正常的。这一步会下载材料，**不会启动主写或复审**；启用飞书时会推送未读公告，并在保存成功回执后标为已读。

### 6. 启动常驻进程，查看第一份产物

```sh
avatarthu service start
avatarthu status
avatarthu keepalive status
```

后台会开始处理已发现的待交作业，**不需要等 12 小时**；12 小时是后续课程扫描间隔。首次使用会处理当前扫描发现的全部符合条件的待交作业。若只想先试一份，暂不启动后台，用 `avatarthu run --task 作业编号` 在前台处理；编号可从 `status` 获取。

初次启动后再次运行 `status`：调度进程应为 `running`，网络学堂应为 `valid`；如启用飞书，`actions`、`messages` 连接应为 `ready`。复审通过后作业进入 `awaiting`，可从 `status` 打开本地审阅页，或在飞书收到卡片并打开云文档。需要本人补充时会显示 `needs_student`，执行失败时显示 `failed` 及原因。没有新作业时不会凭空生成审阅卡片。

看到后台运行后可以关闭终端，也可以退出负责安装的 Agent。系统服务会在用户登录后自动启动；电脑休眠期间暂停，唤醒后补查。只有本人对当前版本的卡片确认提交才会上传作业；本地模式需本人在学校网页提交。

| 首次运行遇到的问题 | 处理方式 |
| --- | --- |
| 找不到 `avatarthu` 命令 | 新开终端，或使用初始化打印的完整路径；不要重复安装语言运行时。 |
| `doctor` 检查未通过 | 按对应 CLI 的提示处理安装、登录或版本问题，再运行 `doctor`。 |
| 学校会话不是 `valid` | 运行 `avatarthu login thu` 完成本人认证，再运行 `avatarthu keepalive run`。 |
| 服务已注册，但没有运行或任务报错 | 查看 `avatarthu status` 和 `~/.avatarthu/logs/daemon.log`。Linux 后台依赖可用的 systemd 用户服务；没有时可用 `avatarthu daemon` 前台运行，关闭终端后会停止。 |
| 飞书连接未就绪 | 查看日志中的权限、授权或网络错误，再运行 `avatarthu login lark`；`ready` 表示监听连接就绪，实际收发以收到卡片和修改反馈为准。 |

<a id="agent-setup"></a>

## 复制给 Agent 自动配置

把下面整段复制给能操作本机终端的 Agent，例如 Claude Code 或 Codex。代码块右上角可一键复制；可先修改前三项偏好。Agent 可以完成下载安装、检查、配置和后台启动，**学校登录、扫码、双因素认证及系统授权仍由你本人完成**。

```text
请在这台电脑上安装并配置 AvatarTHU，实际执行配置，不要只给教程。
项目：https://github.com/Sskift/AvatarTHU
先阅读仓库当前 README.md（英文可读 README.en.md），以实际版本的命令和帮助为准。

我的偏好（仅首次安装时作为默认；已有配置先保留）：
- 分工：claude-codex（Claude 主写，Codex 复审；也可改为 codex-claude）
- 课程扫描：12h；每版最多复审 3 轮；保活使用内置 10 分钟间隔
- 飞书：关闭（可改为开启，向我本人发送卡片和云文档）

请按顺序完成：
1. 检查操作系统、CPU 架构、已有 avatarthu、Claude Code、Codex CLI、浏览器和可选 Lark CLI。保留已有数据、登录和默认模型，不覆盖已有作业或账号配置；已有安装只补齐缺项。若已有后台在运行，报告状态并跳过首次安装、同步与启动。
2. 如未安装 AvatarTHU，从本项目 GitHub Releases 选择最新可用的非草稿版本，包括预发布版。选择匹配系统与架构的压缩包，用同版本 SHA256SUMS 校验后解压。不要依赖 /releases/latest 一定存在，不要从第三方下载，也不要为运行 AvatarTHU 安装 Python、Go 或克隆源码编译。
3. 使用解压后的程序执行 init --no-login --no-start。检查 avatarthu --version；当前终端 PATH 未刷新时使用安装输出的完整路径继续。数据统一通过 ~/.avatarthu 访问，Windows 使用 %USERPROFILE%\.avatarthu；不要放进仓库。
4. 运行 avatarthu tools。复用已有 Claude Code 和 Codex CLI，缺失时按各自官方方式安装。引导我在各 CLI 中完成登录，再运行 avatarthu doctor；两种工具都要可用。沿用各 CLI 默认模型，不修改或比较模型。不要把工具安装成功说成作业已经验证正确。
5. 全新安装按上述偏好执行 avatarthu configure --mode claude-codex --poll-interval 12h --max-review-rounds 3（若我修改了偏好，相应调整参数）；已有配置保留原分工和间隔，除非我明确要求更改。运行 avatarthu login thu，再运行 avatarthu keepalive run，确认 state=valid。密码、验证码和扫码由我在官方登录界面完成，不要要求我把凭据贴进聊天。
6. 若选择飞书，执行 avatarthu login lark --no-start，复用现有登录或引导我完成应用配置与授权；缺少可选依赖时明确说明。若选择本地模式，全新安装保持飞书关闭；不要擅自修改已有飞书绑定。
7. 后台尚未运行时，执行 avatarthu run --sync-only，再用 avatarthu status 检查课程和作业列表。此步骤不启动写作或复审；启用飞书会推送未读公告并在成功后标已读。不要做真实作业提交测试。
8. 两种模型 CLI 和学校登录就绪后，执行 avatarthu service start 并检查 avatarthu status、avatarthu keepalive status。确认调度进程 running、学校会话 valid；选择飞书时还应检查 actions/messages 是否 ready。后台将处理已发现的待交作业并使用我的 CLI 额度；以后按设定间隔扫描，无需负责安装的 Agent 一直在线。
9. 汇报实际版本、安装和数据路径、主写/复审分工、扫描/保活间隔、飞书是否启用、后台状态、已有审阅入口及停止命令 avatarthu service stop。任何步骤受阻，都说明具体原因和下一条操作，不把未完成项说成已完成；缺少工具时可停在仅同步阶段。

提交作业必须等待我本人对当前版本的卡片操作；不要点击提交按钮或模拟回调。保留每轮独立复审意见，不用虚构结果展示配置成功。不要输出 Cookie、token、应用密钥或个人课程内容。
```

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

当前为 **Alpha**，适合愿意自行审阅作业、反馈问题的使用者试用。需要长期跨账号验证学校端变化、不同课程材料与各平台浏览器认证。仓库与 Release 已公开，欢迎试用和提交 Issue。

网络学堂相关接口和 macOS 会话导入改写自 AutoThu 及我们的登录、保活贡献，已经内置在本项目中。来源与许可证见 [THIRD_PARTY.md](THIRD_PARTY.md)。
