# 进度与验证

状态：已完成。日期：2026-09-28。

## 范围确认

用户确认首批 TXT/Markdown/PDF/DOCX 导入、Markdown/DOCX 导出，并补充要求 Excel/CSV；本期增加 XLSX 与 CSV 导入。扫描件 OCR、旧二进制 DOC/XLS、图片理解、动态浏览器登录、PDF 导出留待后续。

## 依赖与依据

- [PDF reader](https://github.com/ledongthuc/pdf)：页级文本提取。
- [Excelize](https://xuri.me/excelize/en/workbook.html)：工作表读取、缓存值与解压限额。
- [Go Readability](https://codeberg.org/readeck/go-readability)：提取网页正文。
- [Bing 搜索 RSS](https://blogs.bing.com/search/2005/1/RSS-Feeds-for-Search-Results/)：公开搜索；本机探测返回 go.dev 等真实结果。公开端点无可用性保证，失败时提示改用直接链接。

## 实现与自动验证

- schema10 资料快照/片段/模型读取记录，六类解析、会话资料面板和点击原文、四个默认工具、正式引用校验、选定版本 MD/DOCX 导出已接通。
- `make test` 通过：Go 全套、TypeScript、15 个前端测试；`go vet ./...` 与 `go test -race ./internal/...` 通过。
- 覆盖六类文件、Excel 缓存值和工作表行、CSV 编码与记录、超限/损坏、来源隔离/未读/伪造摘录、取消后的迟到保存、成员共享、续改继承、旧版导出与来源附录。
- `TONGXI_WEB_SMOKE=1 go test ./internal/material -run TestLivePublicWeb -v` 真实联网通过：Bing 返回 6 条、Go 官方文档正文 149 段。当前网络启用了 VPN 假 IP，通过固定 HTTPS DNS 解析回真实公网地址后仍按公开地址规则连接。
- 使用 bundled LibreOffice 渲染实际 DOCX 生成器样例，逐页检查中文、表格、列表、链接：1 页无裁切。QA 运行时缺中文字体，通过本地 fontconfig 引用系统字体修正；该配置不进入安装包。


## 原生桌面与真实 Kimi 验收

验收会话「资料引用验收 0.1.7」，沿用系统钥匙串中的 Kimi Code 连接，未更改角色或模型配置。

1. 原生多选一次导入 TXT、MD、PDF、DOCX、XLSX、CSV 六份文件；资料面板显示 6 份。Excel 原文显示工作表「预算」第 1–5 行，缓存合计 96；各格式正常保存。
2. Kimi 实际调用三次 read_source 读取活动要求、预算、报名，调用字符统计后通过 advance_work 交付：12 人，预算 200、支出 96、余额 104；9 条正式引用通过。点击成果中的预算引用定位到 Excel 第 5 行，嵌套资料窗口关闭后回到成果。
3. 界面直接添加 https://go.dev/doc/，保存真实正文。新任务让 Kimi 自主 search_web 和 read_web，生成 Go 简介，5 条正式引用通过；新任务独立，不继承读书会的验收条件。
4. 从历史读书会成果导出 Word 和 Markdown；内容、表格与 9 条来源附录齐全。取消一次 Markdown 保存，确认对应文件没有生成，再次导出成功。自动测试另外覆盖同任务旧 V1 不混入 V2。
5. 导出的真实 Markdown 经同一最终 DOCX 生成器重新渲染，检查全部 3 页；来源名称、位置、时间和摘录保持成组分页，中文/表格/链接无裁切。修复 Markdown 转义在 Word 中被原样显示的问题，增加回归测试。
6. 正常退出并安装最终包后重启，8 份来源（6 文件、2 网页）、两个新成果及引用均保留，网页引用可打开原文快照。

## 数据保留与发布

- 升级前正常退出，备份至本地忽略目录 `.local/before-0.1.7-1790575308849267000/`（应用 0.1.6、schema9 数据）。7 张核心表中全部升级前记录逐行比对不变：4 角色、1 模型、11 会话、83 消息、66 Run、3 任务、4 版本。
- 验收后 schema10：4 角色、1 模型、12 会话、87 消息、68 Run、5 任务、6 版本、8 资料快照、22 条片段读取记录；无运行中/排队任务。模型凭据保持系统钥匙串存储。
- `make release` 构建并验证只读 DMG；应用通过 ad-hoc 签名校验，最终包安装到 `/Users/yy/Applications/Tongxi.app`。未提交或推送代码。
- 最终产物 `build/bin/Tongxi-0.1.7-macos-arm64.dmg`，SHA-256：`db4de68a07c2dca58c38c8edeee9f35da3b126d2663b0d4b339ea219afeab6ab`。
- 验收文件、渲染页和数据统计仅在 `.local/sources-0.1.7/`，不进入版本控制。

## 已知边界

扫描件不做 OCR；旧 DOC/XLS、图片/图表理解、动态登录网页、PDF 导出不在本期。Excel 使用保存的缓存结果，不重算公式。网页搜索为公开 RSS 端点，可用性依赖网络；失败可直接添加链接。来源校验保证读过和摘录存在，不保证模型结论的语义或事实正确。每个来源快照保存解析文字，不备份原文件。
