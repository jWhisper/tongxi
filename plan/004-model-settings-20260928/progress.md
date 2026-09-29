# 集中模型设置进度

状态：已完成。版本：0.1.4。更新日期：2026-09-28。

- 已完成 schema 8、共享模型配置与角色 model_id 绑定。
- 已完成预设、配置管理、表单下连接测试，以及角色模型选择。
- 已通过 `make test`、`go test -race ./internal/...`、`go vet ./...` 和 Wails 生产构建；后续只补充保存失败凭据清理测试并单独通过。
- 后端覆盖 schema 7→8 迁移、共享与独立密钥、配置修改、版本冲突、在用模型删除保护、角色按所选配置调用、错误脱敏、空响应、禁止重定向、退出取消和失败回滚。
- 本机原生验收：旧配置自动合并为 1 个 Kimi Code 模型并关联 4 位角色；点击「测试连接」实际收到 Kimi 响应，用时约 0.9 秒。
- 原生预设验收：下拉包含 OpenAI 兼容、DeepSeek、Kimi 开放平台、Kimi Code、GLM；选择 Kimi 开放平台后地址和模型自动填充，未复用已有 Code 密钥。
- 原生多配置验收：以临时本地端点添加第二个模型，测试当前草稿成功、保存成功；角色下拉可选两条模型配置，无 URL 或密钥输入框。重启后两个配置和原角色选择均保留。
- 原生失败与清理：临时端点使用错误密钥显示 401 连接失败；删除无角色引用的临时配置成功，Kimi 配置保留且删除按钮禁用。临时端点进程已退出。
- 升级前已在应用退出后备份应用及数据：`.local/before-0.1.4-1790566593997132000/`。迁移前后均为 4 个角色、9 个会话、73 条公开消息、61 条运行记录、6 条旧验证记录，外键违规为 0。连接测试未新增聊天或验证历史。
- 已安装到 `/Users/yy/Applications/Tongxi.app`，当前页面停留在模型设置。安装包通过 ad-hoc 签名和 DMG 校验，SHA-256 校验通过。

交付包：`build/bin/Tongxi-0.1.4-macos-arm64.dmg`。
SHA-256：`299ddeeeed07ace9c0a1a85d9b181d447c143700a680e4e2f25ef16b5715c6d6`。

本次只使用已有 Kimi 真实凭据及临时本地测试端点。其他服务商预设依据官方文档，未使用其真实凭据调用；Apple 公证和其他平台不在本次验收范围。

预设依据（2026-09-28 核对，均可手动修改）：

| 服务商 | Base URL | 预填模型 | 官方来源 |
| --- | --- | --- | --- |
| OpenAI 兼容 | 用户填写 | 用户填写 | 兼容接口由用户指定 |
| DeepSeek | `https://api.deepseek.com` | `deepseek-flash` | [首次调用](https://api-docs.deepseek.com/guides/agent_integrations/openclaw) |
| Kimi 开放平台 | `https://api.moonshot.cn/v1` | `kimi-k3` | [快速开始](https://platform.moonshot.cn/docs/guide/start-using-kimi-api) |
| Kimi Code | `https://api.kimi.com/coding/v1` | `kimi-for-coding` | [官方概览](https://www.kimi.com/code/docs/) |
| GLM | `https://open.bigmodel.cn/api/paas/v4` | `glm-5.3` | [OpenAI 兼容接入](https://docs.bigmodel.cn/cn/guide/develop/openai/introduction) |

预设不等于所有服务商均已实测。普通连通性成功也不等于流式工具调用已验证。
