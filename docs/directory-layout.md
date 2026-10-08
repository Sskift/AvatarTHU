# Directory layout

每份作业完成后，根目录只保留四个入口。所有新建的文件名与目录名使用英文 ASCII 字符，包括中间过程路径；界面和文档正文仍可使用中文。

```text
assignment/
├── submission/
├── submission.zip
├── workspace/
└── presentation/
```

- `submission/`：当前要交给老师的最小完整文件集。保留完整程序、必要依赖和相对路径；报告不重复放多种格式，除非题目要求。
- `submission.zip`：与该文件夹内容一致的单层压缩包。解压一次即可使用。单文件作业的自动上传仍可直接使用原文件。
- `workspace/`：原题与附件、草稿、实验数据、日志、环境、每轮执行现场、修改记录、历史提交副本以及程序状态。
- `presentation/`：AvatarTHU 展示和编辑所需的报告源文件、HTML/PDF、独立下载件、各版本冻结原件、复审页面与回执。

首次完成前可以暂时只有 `workspace/` 与 `presentation/`。不要在根目录重新增加 `outputs/`、`exports/`、`runs/`、`editor/`、`source/` 或其他“最终版本”入口。

## Internal paths

```text
workspace/
├── source/
├── runs/rN/round-M/
├── editor/
├── work/activity-YYYYMMDD/
├── exports/rN/
├── submission-history/
├── assignment.json
├── state.json
└── submission.json

presentation/
├── outputs/rN/artifacts/
├── outputs/rN/submission.zip
├── outputs/rN/review.md
└── reviews/rN/
```

自动主写返回的 `files` 包含全部展示和编辑下载件，`submission_files` 明确选择实际提交项。两者分开；系统只将选中的文件合并为最终提交包。复审者收到原题、当前候选文件和提交清单；清单变化后重新复审，不沿用旧清单的结论。旧结果没有该字段时保留原有打包行为，避免改变冻结版本。

报告源 ZIP、复核日志、原始数据和生成工具留在 `workspace/` 或 `presentation/`。除非题目明确要求或程序运行必需，不加入最终提交清单。程序必需的压缩数据与 Office/JAR 文件保留原生格式。

## Locate and verify

`avatarthu files ID` 校验冻结原件及当前提交副本，打印顶层 `submission/`。`avatarthu export ID` 返回旁边的 `submission.zip`。`--artifact NAME` 单独导出的下载件放在 `workspace/exports/rN/`；编辑器 HTML 导出放在 `workspace/editor/exports/`。

正常新版本更新最终入口前，先验证现有副本没有被修改，再将上一版副本收进 `workspace/submission-history/`。有改动则报错并保留，不覆盖。文件查看和手动导出不会修改作业状态、卡片或回执，也不会上传作业。

手动筛选的最小提交包可以登记在 `workspace/submission.json`，记录来源版本及哈希；`files` 和默认 `export` 使用同一份记录。该副本不自动替换旧卡片绑定的冻结文件。生成新的正式卡片仍走新版本及独立复审流程。

## Legacy compatibility

旧作业整理时，把旧现场收进 `workspace/legacy/`，展示资料收进 `presentation/`，保留旧绝对路径可访问，并记录目录映射。兼容入口位于旧位置；新作业根目录仍只有四项。不得因整理目录而改写旧文件字节、回执或审批信息。旧现场里已有的名称属于历史记录，新建路径全部使用英文。

课程实体目录使用 `course-<id>/`。迁移旧课程时，在 `.course-layout.json` 保存原始 `identity_dir`，让工作台课程 ID 和已有链接保持不变；旧课程名称只作为兼容链接保留。

macOS 新安装的数据实体仍在 `~/.local/share/avatarthu`，`~/.avatarthu` 是友好入口；已有物理数据根保持兼容。Windows 使用 `%USERPROFILE%\.avatarthu`。仓库只保存源码、文档、测试和许可证，所有课程和运行数据都在 Git 外。
