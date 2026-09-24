# README screenshots / 截图说明

所有截图均使用虚构的“最短路径实验”，不包含真实课程、学生、账号或模型执行记录。两轮复审评论为演示内容，不代表真实模型复审或正确性证据。

| 文件 / File | 来源 / Source |
| --- | --- |
| `review-card.png` | `internal/app/cards.go` 生成演示 JSON，使用 [Open Feishu Card renderer](https://github.com/ztxtxwd/open-feishu-card-monorepo) 0.2.21 在本地渲染后截图；实际飞书客户端的输入框、折叠控件等细节可能不同。 |
| `review-document.png` | `internal/app/document.go` 生成的演示文档在飞书中的题目与结果正文。 |
| `review-files.png` | 同一演示文档中的原生附件。 |
| `review-history.png` | 同一演示文档中的两轮虚构复审评论。 |

文档截图仅保留正文，截图视图排除了浏览器、账户导航、作者姓名和头像。没有改动真实作业文档，也没有公开演示文档的私人地址。PNG 不嵌入原始页面 HTML、附件、会话状态或隐藏的个人信息。

The screenshots use a fictional shortest-path assignment and fabricated review comments. They do not show real students, accounts, coursework, or model execution results.

The card is a local preview of JSON produced by the application's card generator, rendered with Open Feishu Card 0.2.21. Small details such as inputs and collapsible controls may differ in the actual Feishu client. The other images capture the body of a real Feishu document created by the application's document generator using only demo data.

Browser chrome, account navigation, author names, and avatars are excluded from the document capture view. Real assignment documents were not changed, and private document URLs are not published. The PNGs do not embed page HTML, attachments, session state, or hidden personal data.

To refresh the screenshots, generate a separate fictional assignment, use the current card/document builders, capture only the safe content, and inspect every exported image before committing. Keep temporary drafts, renderer dependencies, cloud receipts, and browser data outside the repository.
