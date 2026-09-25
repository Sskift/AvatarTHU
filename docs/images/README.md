# README screenshots / 截图说明

卡片与云文档截图使用虚构的“最短路径实验”，不包含真实课程、学生、账号或模型执行记录。两轮复审评论为演示内容，不代表真实模型复审或正确性证据。菜单栏截图由项目维护者提供，展示实际界面与服务状态，不包含姓名、账号、课程名称或私人文档链接。

| 文件 / File | 来源 / Source |
| --- | --- |
| `review-card.png` | `internal/app/cards.go` 生成演示 JSON，使用 [Open Feishu Card renderer](https://github.com/ztxtxwd/open-feishu-card-monorepo) 0.2.21 在本地渲染后截图；实际飞书客户端的输入框、折叠控件等细节可能不同。 |
| `review-document.png` | `internal/app/document.go` 生成的演示文档在飞书中的题目与结果正文。 |
| `review-files.png` | 同一演示文档中的原生附件。 |
| `review-history.png` | 同一演示文档中的两轮虚构复审评论。 |
| `menubar.png` | 项目维护者提供的 macOS 菜单栏原始截图，展示四项服务指示灯、保活与扫描安排、作业数量和操作入口。 / Maintainer-provided screenshot of the actual macOS menu bar menu. |

文档截图仅保留正文，截图视图排除了浏览器、账户导航、作者姓名和头像。没有改动真实作业文档，也没有公开演示文档的私人地址。PNG 不嵌入原始页面 HTML、附件、会话状态或隐藏的个人信息。

The card and cloud document screenshots use a fictional shortest-path assignment and fabricated review comments. They do not show real students, accounts, coursework, or model execution results. The menu bar screenshot was supplied by the maintainer and shows the actual interface and service status, without names, accounts, course titles, or private document links.

The card is a local preview of JSON produced by the application's card generator, rendered with Open Feishu Card 0.2.21. Small details such as inputs and collapsible controls may differ in the actual Feishu client. The cloud document images capture the body of a real Feishu document created by the application's document generator using only demo data.

Browser chrome, account navigation, author names, and avatars are excluded from the document capture view. Real assignment documents were not changed, and private document URLs are not published. The PNGs do not embed page HTML, attachments, session state, or hidden personal data.

To refresh the screenshots, generate a separate fictional assignment, use the current card/document builders, capture only the safe content, and inspect every exported image before committing. Keep temporary drafts, renderer dependencies, cloud receipts, and browser data outside the repository.
