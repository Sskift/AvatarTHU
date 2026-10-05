# 目录规范

仓库只保存源码、文档、测试和许可证。课程材料、运行状态、账号、日志、实验数据与作业产物都放在 Git 外的数据根目录。

## 数据根与旧路径

- macOS 新安装：数据实体为 `~/.local/share/avatarthu`，`~/.avatarthu` 是访问它的符号链接。
- Windows：使用 `%USERPROFILE%\.avatarthu`。
- 已有安装：保留原数据根，包括实体目录形式的 `~/.avatarthu`；不为统一目录而迁移、复制或删除历史数据。`courses/` 可以继续指向历史 `data/courses/`。
- 自定义安装：用 `AVATARTHU_HOME` 指定独立数据根。

下文的路径均相对于一份作业的目录：`courses/学期/课程名--标识/homework/作业名--标识/`。先运行 `avatarthu status` 获取作业编号，再用 `avatarthu files 作业编号` 查看记录中的实际路径，不凭目录名称猜测当前版本。

## 新作业与新版本

```text
作业目录/
├── source/                     # 原题、说明、原始附件
├── runs/rN/round-M/             # 主写、复审等阶段各自隔离的 job
├── outputs/rN/
│   ├── artifacts/              # 独立下载件和报告编辑包
│   ├── submission/             # 实际提交内容集合
│   ├── submission.zip          # 与 submission/ 同级的单层提交包
│   └── review.md               # 本版主写说明，暂留版本根目录
├── reviews/rN/                 # 每次独立复审记录、审阅文档和回执
├── editor/                     # 可编辑草稿、图片、请求及定稿快照
│   └── exports/                # 编辑器草稿导出，例如 report.html
├── work/活动-日期/             # 手动实验或临时加工，如 verify-20261006
├── exports/rN/                 # 手动导出的提交包
└── state.json                  # 程序管理的作业状态
```

按用途选择目录：

| 用途 | 写入位置 | 规则 |
| --- | --- | --- |
| 下载题目和原始附件 | `source/` | 保留原始文件字节；加工副本放入 job 或 `work/`。 |
| 自动主写、独立复审 | `runs/` 下各自的 job | 每阶段使用隔离目录与新会话。 |
| 发布报告、项目包、报告源文件包等独立下载件 | `outputs/rN/artifacts/` | 冻结后保留原件；修改通过新版本完成。 |
| 查看或提交本版内容 | `outputs/rN/submission/` | 只放实际提交内容，保留所需相对路径、执行权限和原生格式。 |
| 保留独立复审 | `reviews/rN/` | 每次已完成的复审单独保留；`outputs/rN/review.md` 不替代这些记录。 |
| 编辑正文、接收局部建议、保存定稿快照 | `editor/` | 草稿与冻结产物分开。 |
| 手动验证、补跑实验、临时制作 | `work/<activity>-<date>/` | 按活动和日期分目录，不混入 `source/` 或冻结版本。 |
| 导出编辑器草稿 HTML | `editor/exports/` | 保留草稿和冻结原件，导出供阅读的副本。 |
| 另行导出提交包 | `exports/rN/` | 按版本保存手动导出的副本，保留冻结原件。 |

提交包只有一层：解压 `submission.zip` 一次即可找到报告、可运行程序和源码。独立下载用的项目包和报告编辑包在形成提交内容时展开，不在最终包中重复嵌套；程序必需的压缩数据保持原生格式。只有一个非 ZIP 文件的作业仍直接提交该原文件，不额外包装成 ZIP。`submission/` 用于查看该版本的实际提交内容，不是修改冻结作业的入口。

## 查找与校验文件

```sh
avatarthu status
avatarthu files 作业编号
```

`files` 先校验记录中的冻结文件，再从已冻结的提交文件补齐缺失的 `submission/`，或校验已有目录，并打印相关路径。它不自动打开文件管理器，不改原件、不重打包、不改作业状态或卡片，也不上传作业。

已有 `submission/` 内容与冻结文件不一致时，命令报错并保留目录，不覆盖其中的改动。需要修改报告或程序时，用 `avatarthu edit 作业编号` 或 `avatarthu revise 作业编号 --feedback "修改意见"` 生成新版本；不要直接改冻结文件来消除校验错误。

需要单独导出副本时使用：

```sh
avatarthu edit --export-html 作业编号
avatarthu export 作业编号
avatarthu export 作业编号 --artifact 项目包.zip
```

旧版平铺的 `outputs/rN/`、旧 `outbox/`、旧审阅目录和已有卡片继续按记录中的路径使用。`files` 可以补齐用于查看的提交目录，但不会移动旧版原件、把它们强制改成新布局，或更新已有卡片。已有的手动工作入口与导出路径也继续保留；新工作按上述用途归档。
