# 同席参考项目与采用边界

资料核查日期：2026-09-27。来源以项目官方仓库和文档为主。

本文件长期维护参考来源及采用边界；各期的具体取舍和变更记录放在对应迭代，入口见 [迭代索引](README.md)。

本文件区分「依赖」「设计参考」「调研背景」。功能描述来自当前公开文档及部分源码阅读，未在本项目中安装或运行这些参考软件；引用不代表其所有能力都已被实测。

## 1. Eino：采用为 Agent 执行框架

- 仓库：[cloudwego/eino](https://github.com/cloudwego/eino)
- [ADK 概览](https://www.cloudwego.io/docs/eino/core_modules/eino_adk/agent_preview/)
- [ChatModelAgent](https://www.cloudwego.io/docs/eino/core_modules/eino_adk/agent_implementation/chat_model/)
- [Agent 协作](https://www.cloudwego.io/docs/eino/core_modules/eino_adk/agent_collaboration/)
- [多轮会话示例](https://www.cloudwego.io/docs/eino/quick_start/chapter_02_chatmodelagent_runner_agentevent/)

采用：ChatModelAgent、Runner、模型组件、工具调用和执行事件。角色和模型配置由同席管理，执行时转换成 Eino 对象。

同席自行实现消息收件人、会话历史、待执行队列与协作次数限制。AgentAsTool 作为后续等待结果的子任务机制，首个场景为主要助手调用评审 Agent 后继续修改，使用独立子任务上下文并关联主执行；Checkpoint 用于未来执行恢复，不替代业务聊天记录。框架 API 以 P0 锁定版本验证为准。

## 2. Hermes Agent：角色、会话和协作体验的主要参考

- 仓库：[NousResearch/hermes-agent](https://github.com/NousResearch/hermes-agent)
- [Bot Mode](https://github.com/NousResearch/hermes-agent/blob/main/website/docs/user-guide/bot-mode.md)
- [Profiles](https://github.com/NousResearch/hermes-agent/blob/main/website/docs/user-guide/profiles.md)
- [Session Storage](https://github.com/NousResearch/hermes-agent/blob/main/website/docs/developer-guide/session-storage.md)
- [Subagent Delegation](https://github.com/NousResearch/hermes-agent/blob/main/website/docs/user-guide/features/delegation.md)

借鉴：具名角色长期存在、角色和会话分开管理、每个成员保持自己的上下文、Agent 通过明确工具联系伙伴、交流记录带署名且可查看。Hermes 的 SQLite 会话存储和 Bot Mode 是具体参考入口。

同席首版采用自己的 Go/Eino 运行时，不直接依赖 Hermes 的 Python 运行时、内部数据库或桌面群聊调度实现。其 profile 目录布局也不照搬为同席的数据模型。

后续若用户确实需要 Hermes 的完整工具能力，可以单独评估其公开 CLI/服务接口；这是一项新的运行时适配需求，不是首版隐含依赖。

## 3. Buzz：共同工作区与显式消息触发的参考

- 仓库：[block/buzz](https://github.com/block/buzz)
- [架构文档](https://github.com/block/buzz/blob/main/ARCHITECTURE.md)
- [Agent 接入层 buzz-acp](https://github.com/block/buzz/tree/main/crates/buzz-acp)
- [消息 CLI](https://github.com/block/buzz/tree/main/crates/buzz-cli)
- [消息与提及实现](https://github.com/block/buzz/blob/main/crates/buzz-cli/src/commands/messages.rs)

借鉴：人和 Agent 在同一会话中交流、有明确身份的消息、指向具体成员的提及，以及「消息送达」与「Agent 开始执行」之间的明确连接。

Buzz 使用独立 Relay 和 Nostr 事件，其部署涉及多种基础设施。同席把保存、路由和调度收敛为本地 Go 模块与 SQLite，不采用 Nostr、公私钥成员体系、独立 Relay、Redis/Postgres 或 Git 托管。

不把 Buzz 的路线图视为同席已具备或必须实现的功能。

## 4. Wails：采用为桌面容器

- 仓库：[wailsapp/wails](https://github.com/wailsapp/wails)
- [官方介绍](https://wails.io/docs/introduction/)
- [运行时与事件](https://wails.io/docs/reference/runtime/intro/)

采用：Go/JavaScript 调用桥、桌面窗口、业务事件和应用打包。前端使用 React/TypeScript。

本次核查官方文档将 v2 作为稳定系列、v3 标为 beta，首版选 v2；P0 已锁定 Wails v2.16.0，完整依赖与验证结果见本期进度。桌面壳选型不要求产品绑定编程用途。

## 5. 调研背景，不作为首版依赖

| 项目 | 调研启发 | 本轮不引入的内容 |
| --- | --- | --- |
| [MCP Agent Mail](https://github.com/Dicklesworthstone/mcp_agent_mail) | 异步消息和交流记录可以与任务执行分开 | 独立 MCP 邮件服务、文件租约、Beads 配套体系 |
| [Paperclip](https://github.com/paperclipai/paperclip) | 查看哪个 Agent 在做什么、任务归属清晰 | 公司组织架构、预算治理、复杂任务平台 |
| [OpenAgents](https://github.com/openagents-org/openagents) | 多 Agent 共用工作区的产品形式 | 多运行时启动器、网络 SDK、云工作区 |

这些项目帮助判断产品边界。首版实现不应为了覆盖它们的全部能力而扩大范围。

## 6. 引用与后续维护

- 上述链接大多指向 main 或滚动文档，内容会变化；实施具体接口时以锁定版本的文档和代码为准。
- 同席业务方案自行实现；`frontend/wailsjs` 绑定、运行时文件及 `build/` 初始打包资源由 Wails CLI v2.16.0 生成，来源为 Wails（MIT 许可）。未复制 Hermes 或 Buzz 的业务源码；后续直接复用其他代码或资源时继续记录来源、版本和许可要求。
- 修改关键方案时，同步更新 [技术架构](architecture.md) 与对应迭代的需求、实施计划和进度记录，并说明是同席自身需求还是外部框架变化导致。
