# AvatarTHU

个人课程助手：每天扫描清华网络学堂，按课程归档公告、课件、作业描述和附件，用一个 Claude Code 会话完成每份作业，生成可打开的本地审阅页。飞书是可选功能：启用后把未读公告发给本人并标为已读，把作业产物集中嵌入云文档，会话只发送交互卡片，由本人决定提交或修改。

AvatarTHU 是独立项目，网络学堂登录、HTTP 客户端和保活已经内置，不需要克隆、安装或运行 AutoThu。相关代码借鉴并改造自 AutoThu，保留 MIT 许可证与完整来源说明，见 [THIRD_PARTY.md](THIRD_PARTY.md)。

默认北京时间 **08:00** 同步。Mac 休眠或关机时暂停，唤醒后补跑；修改请求每分钟检查，不用等到次日。

## 安装与运行

需要 macOS、Python 3.10+、已登录的 `claude` 和 Chrome。安装时会验证网络学堂登录；没有有效登录态时，先尝试导入 Chrome 会话，再打开独立登录窗口，由本人完成学校认证。

首次安装默认不启用飞书，也不检查或安装 Lark CLI。后续更新保留已有的飞书开关。需要推送时，运行 `./avatarthu login lark`：检测不到 CLI 则通过 npm 将官方 `@larksuite/cli` 安装到 AvatarTHU 运行目录；没有应用配置则启动创建向导；没有有效登录态则启动浏览器授权；已有登录态会验证并复用。启用飞书时才需要 Node.js/npm，应用配置或授权仍需本人在浏览器完成。

```sh
./setup.sh                           # 首次安装：本地模式，安装每日任务和内置保活
./setup.sh --lark                    # 安装并启用飞书，引导完成缺失的登录
./setup.sh --no-lark                 # 安装/更新为本地模式
./setup.sh --no-start --skip-login   # 准备环境，交互登录和服务启动留到稍后
./avatarthu login                    # 一键更新网络学堂登录态（等同 login thu）
./avatarthu login thu --import-only # 只尝试导入已有 Chrome 会话
./avatarthu login lark               # 一键配置、登录并启用飞书
./avatarthu notifications off        # 关闭推送及回调，继续本地运行
./avatarthu notifications on         # 重新检测登录并启用推送
./avatarthu status                   # 查看登录、服务、作业与审阅文件路径
./avatarthu run                      # 立即同步并处理作业
./avatarthu run --sync-only          # 只归档、下载；启用飞书时转发公告并标已读
./avatarthu run --task TASK_ID       # 同步后只处理指定作业
./avatarthu revise TASK_ID --feedback "补充推导和验证过程"
./avatarthu uninstall                # 停用全部 AvatarTHU 服务，保留文件
```

原来的 `login.sh`、`status.sh`、`run-now.sh` 和 `uninstall.sh` 仍可使用。独立命令也安装在 `~/.local/share/avatarthu/bin/avatarthu`，可自行将此目录加入 PATH。入口是调用自带虚拟环境的可执行脚本，不需要外部 `thu-learn` 二进制。

重新运行 `setup.sh` 可更新程序。旧版会话一次性迁入 AvatarTHU，后续升级不覆盖新会话；原文件保留。安装启用时会停用旧的 `com.local.thu-learn-loop.*`，并在确认旧配置使用 AutoThu 保活时接管其服务；LaunchAgent 文件备份到运行目录。当前只有 AvatarTHU 自己的 `daily` 和 `keepalive` 是必需服务，`actions`、`messages` 随飞书开关启停。`--no-start` 不修改现有服务的加载状态，适合随后执行 `./setup.sh --agents-only` 完成部署。

## 日常使用

1. **公告**：始终归档到课程目录。本地模式保留学校的未读状态；启用飞书时只转发未读公告，持久保存飞书回执后再打开详情并确认已读。发送失败不改已读状态；标记失败只重试标记。
2. **资料**：归档当前学期所有课程的公告与课件，给未提交且未截止的作业下载原题和附件。文件下载采用临时文件、完整性检查和增量缓存。
3. **执行**：每份作业由一个 `claude -p` 会话完成读题、解题、验证、自查、打包。每次尝试使用新目录，进程成功退出、结构化结果和文件检查通过后，才保存完成记录。已成功的结果可断点恢复。
4. **审核**：本地模式每版生成 `data/reviews/<任务ID>/r<版本>/index.html`，直接打开即可审阅。页面固定分为“作业描述”“完成情况与关键结果”“完整产物”“审阅与操作”四部分，包含关键图、文件链接及自查记录。启用飞书后生成本人拥有的同结构云文档，原题与产物作为附件嵌入，会话只发送一张文档入口与操作卡片。已有本地产物可在下一次调度时发布到飞书，复用同一版文件，不重跑 Claude。
5. **修改**：本地使用 `./avatarthu revise TASK_ID --feedback "修改意见"`。飞书中可以圈选文字、图片或源码写批注，写完后点击卡片“按文档批注修改”，只读取本人未解决的批注；也可以填写卡片意见或回复卡片。下一版逐项响应意见，旧版按钮立即失效。直接编辑审阅文档不改变提交文件。
6. **提交**：本地模式由本人在学校网页提交；没有命令行自动提交入口。飞书卡片点击“审阅完成，提交本版”后，才由 `loop.actions` 校验本人身份、当前消息、版本、随机标识、文件哈希及学校待交状态。收到学校成功回执才显示成功。响应丢失进入 `submission_unknown`，需本人在网页核对，不会自动重传。关闭飞书时，已有卡片的回调也不执行。

题面中的禁止 AI 文案按用户声明的测试场景处理，不作为字符串过滤或自动停工条件。执行器仍需如实记录无法完成的实验、缺失的数据和失败的验证；有未解决问题时，卡片保留修改入口，不显示提交按钮。自查由同一个 Claude 会话完成，不是独立评审。

审阅文档从本版冻结文件生成，展示程序不执行作业代码。同一个 Claude 会话提供题目摘要、最多三张关键结果图的文件引用与观察要点，以及执行自查和待核对事项；不增加第二个模型或评审阶段。旧版本没有这些信息时使用原题描述和已有产物预览。正文仅展示少量关键图或答案节选，PDF 默认以附件预览，无独立图片时显示报告首页；完整文件始终嵌入第三部分。上传后逐项核对附件，缺失时不会发送审阅卡片。原生文档支持批注；要修改附件内容，可在相邻说明中写明文件名、页码或代码位置。当前不提供在飞书里运行桌面程序的功能。

## 文件管理

仓库中的 `courses` 是安装器创建的本地快捷链接，不入 Git：

```text
~/.local/share/avatarthu/
├── config.json                 # 本机设置和本人飞书 ID，权限 0600
├── session.json                # AvatarTHU 自己的网络学堂登录态，权限 0600
├── bin/avatarthu                # 统一命令入口
├── data/
│   ├── courses/<学期>/<课程名>--<ID>/
│   │   ├── course.json
│   │   ├── notices/             # 公告 JSON 与 Markdown
│   │   ├── courseware/          # 原课件、索引和下载记录
│   │   └── homework/<作业名>--<ID>/
│   │       ├── assignment.json
│   │       ├── state.json
│   │       ├── source/          # 描述、附件、题面图片
│   │       └── runs/r1/attempt-<ID>/
│   │           ├── input/      # 本次材料快照、提取文本和图片
│   │           ├── final/      # Claude 生成的实际产物
│   │           ├── review.md   # 自查证据和未解决问题
│   │           ├── claude.json # 进程结果
│   │           └── complete.json
│   ├── tasks/                  # 调度状态
│   ├── reviews/<任务ID>/r1/     # 本地审阅页与结果图预览
│   └── notices.json            # 推送回执与已读记录
├── outbox/<任务ID>/r1/          # 本地与飞书审阅共用的冻结副本
├── draft_<ID>_folder/           # 审阅文档草稿、页面图片和发布回执
└── logs/                       # daily/keepalive/actions/messages 日志
```

每门课程和每份作业的目录名包含稳定 ID，避免重名混淆。所有材料、身份信息、Cookie、日志均排除在 Git 之外。登录态、运行依赖、保活和课程数据都归 AvatarTHU 管理。

## 本机配置

编辑 `~/.local/share/avatarthu/config.json`：

| 字段 | 默认值 | 用途 |
|---|---|---|
| `daily_time` | `08:00` | Asia/Shanghai 每日同步时间 |
| `stage_timeout` | `7200` | 单个 Claude 会话最长运行秒数 |
| `claude_model` | 未设置 | 可选；否则使用 Claude Code 本机默认模型 |
| `lark_enabled` | 首次安装 `false` | 可选飞书推送；建议通过 notifications 命令切换以同步服务状态 |
| `claude_cli` / `lark_cli` | 安装时发现的路径 | CLI 可执行文件 |
| `lark_user_id` | 启用飞书时识别的本人身份 | 唯一消息接收者与提交审批者；本地模式不需要 |

程序不会把学校 Cookie 或飞书配置写入 Claude 输入，禁用该会话的 MCP，并将执行范围写入任务指令。Claude CLI 使用当前用户权限运行；这不是操作系统级隔离环境。

## 保活与排错

内置登录与保活实现在 `loop/thulearn/`：每 15 分钟检查并保存轮换 Cookie；失效时至多每小时尝试从 Chrome 恢复已有会话，后台不会自动弹登录窗口。学校要求重新认证时，运行 `./avatarthu login thu`。保活不能绕过学校的强制过期。

同步或执行失败会记录日志和本地通知；启用飞书时另行通知本人，15 分钟后重试。也可运行 `./run-now.sh`。云文档发布回执会持久保存，卡片发送失败重试时复用已发布文档。卡片回调和回复通过 `lark-cli event consume` 长连接接收，不需要公网回调服务器；需启用 `card.action.trigger` 和 `im.message.receive_v1`。

审阅文档使用 `lark-cli` 的用户身份创建，需具有云文档创建、上传和评论读取权限。文档创建成功即保存 ID，后续读取失败不会新建另一份。若创建请求的响应丢失，状态保留 `review_doc.creating=true`；需检查飞书最近文档并恢复对应 ID/URL 后重试，不盲目重复创建。旧卡片会在后台原位升级展示，版本和提交凭据保持一致。

## 开发验证

```sh
~/.local/share/avatarthu/.venv/bin/python -m unittest discover -s tests -v
```

测试涵盖无飞书安装与执行、本地审阅和修改、按需登录、旧会话迁移、模式切换、公告先送后读、单次 Claude 执行、陈旧/伪造卡片拒绝、文件篡改及未知提交结果等关键路径；真实上传接口全部模拟。

项目基于 AutoThu 和本机原型改造；第三方来源见 [THIRD_PARTY.md](THIRD_PARTY.md)。
