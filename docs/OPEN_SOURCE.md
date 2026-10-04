# Nemi 的开源复用与设计参考

日期：2026-10-04。v0.3 按“能直接复用则接入，架构差异大则借鉴合同”的原则开发；不会把阅读某个项目描述为已经集成它。

## 本轮实际复用

| 组件 | 接入方式 | 用途 / 边界 |
| --- | --- | --- |
| [teambition/rrule-go](https://github.com/teambition/rrule-go) | Go 依赖固定 v1.8.2；MIT | 直接计算 RFC 5545 日历日期。Nemi 仅开放每天、周一至周五、每周，不向模型开放任意规则字符串 |
| [golang-ical](https://github.com/arran4/golang-ical) | 直接依赖 v0.3.7；Apache-2.0 | 单次 iCalendar 事件序列化、转义与折行；不手写 ICS 格式 |
| [Temporal Go SDK](https://github.com/temporalio/sdk-go) | 继续使用 v1.49.0；MIT | 持久计时、任务重放、Activity 重试和唯一工作流 ID；不自行开发持久执行引擎 |
| [pgx](https://github.com/jackc/pgx) | 继续使用 v5.7.6 | PG 事务、连接池和类型化查询，业务事实与幂等仍在 PG |
| [Lucide](https://github.com/lucide-icons/lucide) | 继续使用 lucide-react v0.468.0 | 新的偏好、工作动态、编辑和移除图标，沿用现有视觉组件 |

`rrule-go` 的日期计算接入见 `internal/domain/recurrence.go`。Nemi 自己负责用户确认、结束日期、免打扰、版本检查、漏发策略和通知幂等；这些是应用合同，不是另写一个日历算法。提醒与模型生成使用不同队列，提醒到点不调用 LLM。

新增依赖的完整 [rrule-go MIT notice](./licenses/rrule-go.txt)与现有 [Temporal SDK MIT notice](./licenses/temporal-go-sdk.txt)以及 [golang-ical Apache-2.0 license](./licenses/golang-ical.txt)随仓库保存，并进入后端 Docker 镜像的 `/usr/share/licenses/nemi`。未复制开源项目的业务源码；直接依赖版本和完整传递依赖由 `go.mod` / `go.sum`、npm lockfiles 记录。

## 已调研、作为设计参考

### OpenClaw

[自动任务文档](https://github.com/openclaw/openclaw/blob/main/docs/automation/cron-jobs.md)说明持久保存任务、唤醒和查看运行历史；[记忆文档](https://github.com/openclaw/openclaw/blob/main/docs/concepts/memory.md)区分稳定资料、长期记忆与日常笔记。

Nemi 借鉴“持续任务有独立计划和可查看历史”的产品方式，并把偏好与事项原始资料分开。未接入 OpenClaw Gateway、Markdown workspace、自动记忆整理、渠道自动发送、浏览器或工具执行。它已有 Node 运行时，整体引入会和 Nemi 的 Go / Temporal 生命周期形成双重所有权；当前不这样迁移。

### Mem0

[Mem0 项目](https://github.com/mem0ai/mem0)提供面向 Agent 的持久记忆层；其[管理实现](https://github.com/mem0ai/mem0/blob/main/mem0/memory/main.py)包含按身份范围查询、更新及删除。

Nemi 借鉴显式范围和可管理的记忆接口。v0.2 的偏好只有用户确认的结构化文本，放在现有 PG，不自动发出抽取 / embedding 请求，也不增加向量服务或 Python 主执行器。未安装 Mem0 SDK，没有使用它的云服务、语义检索、关系图和自动提取；需要评估检索质量后再选它或其他检索组件。

### 后续复用顺序

1. 先实现真实供应商的有界工具调用合同，再评估与 Go 主执行器兼容的 Agent SDK；不把另一个无限 Agent Loop 直接嵌进 Worker。
2. 本版三平台群消息复用官方 Webhook 协议与 Go HTTP / crypto 标准库；只发送文字，不为一个端点引入整套办公 SDK。后续用户 OAuth、文档 / 日历读取优先官方 SDK 与授权 API，不把群机器人当作读取权限。
3. 文件流程使用成熟 PDF / OCR 与对象存储组件，同时保留来源和权限；高风险执行进入独立工具池。
4. 需要云端浏览器时评估 Playwright 与现成的沙箱 Broker，单独验证隔离和接管，再开放生活场景。

上述是下一阶段评估顺序，不表示对应能力已经交付。对标基于 dots 的[任务与记忆](https://learn.chatgpt.com/docs/dots/tasks-and-memory)和[控制与活动](https://learn.chatgpt.com/docs/dots/controls)公开合同，不推定其内部实现。


## v0.4 飞书实际复用

直接依赖飞书官方 larksuite/oapi-sdk-go v3.12.0（MIT），复用自建应用授权与 docx raw_content 服务。仅初始化 auth / docx 服务，避免编译无关业务模块；Nemi 负责凭据加密、出站约束、日志脱敏、响应大小、权限错误、确认导入与事务去重。未接入 SDK 的自动渠道发送、隐式重发或 WebSocket 事件。完整许可证存于 licenses/larksuite-oapi-sdk-go-MIT.txt 并随镜像交付。
