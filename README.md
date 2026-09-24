# AvatarTHU

个人课程助手：每天扫描清华网络学堂，把未读公告发到本人飞书并标为已读；按课程下载课件、作业描述和附件，用一个 Claude Code 会话完成每份作业，发送产物和交互卡片，由本人决定提交或修改。

默认北京时间 **08:00** 同步。Mac 休眠或关机时暂停，唤醒后补跑；修改请求每分钟检查，不用等到次日。

## 安装与运行

需要 macOS、Python 3.10+、已登录的 `claude` 和已配置的 `lark-cli`。网络学堂登录支持从 Chrome 导入已有会话，或打开浏览器完成认证。

```sh
./setup.sh                      # 安装运行环境、保活和三个后台服务
./setup.sh --autothu ../AutoThu  # 可复用现有 AutoThu 的 Python 和初始会话
./setup.sh --no-start           # 只准备环境，不启用后台服务
./login.sh                      # 一键更新共享的网络学堂登录态
./status.sh                     # 查看同步、服务、作业状态
./run-now.sh                    # 立即同步并处理作业
./run-now.sh --sync-only        # 只归档、下载、转发公告和标已读
./run-now.sh --task TASK_ID     # 同步后只处理指定作业
./uninstall.sh                 # 停用 AvatarTHU，保留文件和共享 AutoThu 保活
```

重新运行 `setup.sh` 可更新程序。安装启用时会停用旧的 `com.local.thu-learn-loop.*` 服务，并备份它们的 LaunchAgent 文件；旧资料和产物保留，避免两套 loop 重复处理和推送。

## 日常使用

1. **公告**：只转发网络学堂标记为未读的公告；飞书返回消息回执后，打开对应公告详情并再次查询，确认学校已标为已读。发送失败不改已读状态；标记失败只重试标记。
2. **资料**：归档当前学期所有课程的公告与课件，给未提交且未截止的作业下载原题和附件。文件下载采用临时文件、完整性检查和增量缓存。
3. **执行**：每份作业由一个 `claude -p` 会话完成读题、解题、验证、自查、打包。每次尝试使用新目录，进程成功退出、结构化结果和文件检查通过后，才保存完成记录。已成功的结果可断点恢复。
4. **审核**：文件作为飞书附件发给本人，随后发送 Card 2.0 卡片，列出截止时间、版本、产物、自查摘要和待补充项。点击“确认，提交本版产物”才提交；填写修改意见或回复卡片，会生成新版本并让旧卡片立即失效。
5. **提交**：校验本人身份、当前消息、版本、随机标识和文件哈希，重新检查学校的待交状态、截止时间、题目和附件版本。收到学校成功回执才显示提交成功。若响应丢失，进入 `submission_unknown`，需本人在网页核对，不会自动重传。

题面中的禁止 AI 文案按用户声明的测试场景处理，不作为字符串过滤或自动停工条件。执行器仍需如实记录无法完成的实验、缺失的数据和失败的验证；有未解决问题时，卡片保留修改入口，不显示提交按钮。自查由同一个 Claude 会话完成，不是独立评审。

## 文件管理

仓库中的 `courses` 是安装器创建的本地快捷链接，不入 Git：

```text
~/.local/share/avatarthu/
├── config.json                 # 本机设置和本人飞书 ID，权限 0600
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
│   └── notices.json            # 推送回执与已读记录
├── outbox/<任务ID>/r1/          # 发到飞书的冻结副本
└── logs/                       # daily/actions/messages 日志
```

每门课程和每份作业的目录名包含稳定 ID，避免重名混淆。所有材料、身份信息、Cookie、日志均排除在 Git 之外。网络学堂会话共用 `~/.config/autothu/session.json`。

## 本机配置

编辑 `~/.local/share/avatarthu/config.json`：

| 字段 | 默认值 | 用途 |
|---|---|---|
| `daily_time` | `08:00` | Asia/Shanghai 每日同步时间 |
| `stage_timeout` | `7200` | 单个 Claude 会话最长运行秒数 |
| `claude_model` | 未设置 | 可选；否则使用 Claude Code 本机默认模型 |
| `claude_cli` / `lark_cli` | 安装时发现的路径 | CLI 可执行文件 |
| `lark_user_id` | `lark-cli whoami` 的本人身份 | 唯一消息接收者与提交审批者 |

程序不会把学校 Cookie 或飞书配置写入 Claude 输入，禁用该会话的 MCP，并将执行范围写入任务指令。Claude CLI 使用当前用户权限运行；这不是操作系统级隔离环境。

## 保活与排错

复用 AutoThu 的 macOS 登录与保活实现：每 15 分钟检查并保存轮换 Cookie；失效时尝试从 Chrome 恢复已有会话。服务端要求重新认证时，运行 `./login.sh`。保活不能绕过学校的强制过期。

同步或执行失败会记录日志并通知本人，15 分钟后重试；也可运行 `./run-now.sh`。发送中的文件已保存回执，重试从未完成部分继续。卡片回调和回复通过 `lark-cli event consume` 长连接接收，不需要公网回调服务器；需启用 `card.action.trigger` 和 `im.message.receive_v1`。

## 开发验证

```sh
~/.local/share/avatarthu/.venv/bin/python -m unittest discover -s tests -v
```

测试涵盖公告先送后读、失败重试、单次 Claude 执行、陈旧/伪造卡片拒绝、文件篡改、修改请求、未知提交结果等关键路径；真实上传接口全部模拟。

项目基于 AutoThu 和本机原型改造；第三方来源见 [THIRD_PARTY.md](THIRD_PARTY.md)。
