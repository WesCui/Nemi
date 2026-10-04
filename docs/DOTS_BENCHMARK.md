# dots 功能对标、实现边界与国内个人市场选择

日期：2026-10-04 · 调研口径：官方公开文档 · 项目目标：国内大众个人用户的生活助理

本文配合 [PRD](./PRD.md) 和 [架构文档](./ARCHITECTURE.md)。功能对标是产品规划，不代表已经实现。

## 1. 调研结论

OpenAI 官方把 dots 描述为持续承担职责的 Agent，并提供云端计算环境、任务委派、记忆和用户决策入口。它的完整体验涉及任务执行、计算环境、交互渠道和权限治理。[Meet dots](https://learn.chatgpt.com/docs/dots)

**本次官方资料检索没有找到足以确认 dots 后端语言、内部数据库、消息队列、工作流引擎或沙箱虚拟化技术的证据。不能断言 dots 使用 Go、Python、Temporal、Kubernetes 或 Firecracker。**

公开行为能帮助我们确定需要解决的问题，但不能反推出唯一实现。以下“我们的组件”是独立工程选择，不是对 OpenAI 内部架构的揭秘。

## 2. 官方可确认的功能与对标计划

“P0”是生活助理验证版；“P1”是消费级内测对标版；“P2”是进一步扩展。版本边界以当前 PRD 为准。

| 功能 | dots 官方公开行为 | 国内版对应能力 | 阶段 | 官方依据 |
| --- | --- | --- | --- | --- |
| 长期职责 | 对话间持续跟进工作 | 每位用户一个生活助理与多个“正在帮我办的事” | P0 | [Tasks](https://learn.chatgpt.com/docs/dots/tasks-and-memory) |
| 继续沟通与改方向 | 后台工作期间可补充信息 | 对话更新目标，版本化变更当前事项 | P0 | [Tasks](https://learn.chatgpt.com/docs/dots/tasks-and-memory) |
| 定时与事件 | 固定计划、支持来源的事件监控 | 待办提醒、到期检查、授权来源事件 | P0 | [Tasks](https://learn.chatgpt.com/docs/dots/tasks-and-memory) |
| 自主暂停和唤醒 | 工作需要时暂停，后续继续 | 有边界的下一次检查建议，保存后定时唤醒 | P1 | [Tasks](https://learn.chatgpt.com/docs/dots/tasks-and-memory) |
| 并行委派 | 背景 Agent 和独立任务 | 比较资料、整理清单等有界 Child Run | P1 | [Tasks](https://learn.chatgpt.com/docs/dots/tasks-and-memory) |
| 长期记忆 | 保存偏好、决定和进行中的工作 | 用户可查看和纠正的生活偏好、事项状态 | P0 | [Tasks](https://learn.chatgpt.com/docs/dots/tasks-and-memory) |
| 只读主动研究 | 读取获准信息并形成建议 | 获得授权后检查事项变化，无变化不打扰 | P1 | [Tasks](https://learn.chatgpt.com/docs/dots/tasks-and-memory) |
| 云端电脑 / 浏览器 | 独立文件、软件、网站会话 | 按需分配的个人云端工作区与隔离浏览器 | P1 | [Computers](https://learn.chatgpt.com/docs/dots/computers-and-apps) |
| 用户接管 | Take over / Return control | Web 接管浏览器，控制权排他交接 | P1 | [Computers](https://learn.chatgpt.com/docs/dots/computers-and-apps) |
| 网站登录 | 私密登录流程，云浏览器会话独立 | 登录信息避开聊天；验证码和敏感步骤交给用户 | P1 | [Computers](https://learn.chatgpt.com/docs/dots/computers-and-apps) |
| 用户本地电脑 | 另行授权，设备在线才可使用 | 可选桌面连接器，单设备明确授权 | P2 | [Computers](https://learn.chatgpt.com/docs/dots/computers-and-apps) |
| 应用工具 | 接入已授权插件 | 用户导入、官方接口和审核工具目录 | P0 / P1 | [Computers](https://learn.chatgpt.com/docs/dots/computers-and-apps) |
| 多渠道连续身份 | ChatGPT / Slack / Teams 联系同一个 dot | 小程序与 Web 绑定同一个助理，分别保留会话 | P0 / P1 | [Channels](https://learn.chatgpt.com/docs/dots/channels) |
| 语音 | 用户可发起通话 | P0 按住说话转文字；P1 实时中文语音 | P0 / P1 | [Channels](https://learn.chatgpt.com/docs/dots/channels) |
| 查看进度与结果 | Activity 呈现任务、文件和待决定事项 | “在办的事 / 需要你决定 / 已办好” | P0 | [Controls](https://learn.chatgpt.com/docs/dots/controls) |
| 动作审查 | 授权范围、自动审查、必要时用户决定 | 确定参数审批、当前授权复查与副作用核对 | P0 | [Controls](https://learn.chatgpt.com/docs/dots/controls) |
| 个性化外观 | 名称和外观可定制 | 名称和基础头像；复杂动画以后验证 | P0 / P2 | [Meet dots](https://learn.chatgpt.com/docs/dots) |

当前渠道文档把主动发起电话和短信列为后续能力，不能写成已经全面支持；产品可用范围也受 rollout 和客户端限制。[Message your dot](https://learn.chatgpt.com/docs/dots/channels)

本项目不以调用 dots 或代接 OpenAI 账户作为底座，也不把多模型接入列为已确认的 dots 功能。国内模型可替换、消费额度和国内渠道是我们自己的产品选择。

## 3. “如何实现”中可以确认与不能确认的部分

| 问题 | 本次公开资料能确认 | 不能从资料确认 |
| --- | --- | --- |
| 工作在哪里发生 | 有云端工作及另行授权的本地执行；本地步骤需要设备可用 | 具体服务部署拓扑与语言 |
| 云端资源 | 用户能看到独立电脑、文件和网站会话 | 是否每用户一台常驻 VM、共享容器还是混合池 |
| 任务如何组织 | 存在后台任务与委派 | 内部 Task / Run schema、队列或 Workflow 引擎 |
| 记忆如何保持 | 相关上下文与保存的笔记用于后续工作 | 向量数据库、图数据库或存储产品 |
| 权限如何工作 | 操作受指令、授权及审查约束 | 审查模型、规则引擎实现与调用链 |
| 多端如何协同 | 云端协调与本地工具执行可分开 | 传输协议、内部 RPC、网络拓扑 |

云端协调与本地执行的公开边界可参考管理员说明。[Local computer access](https://learn.chatgpt.com/docs/enterprise/cloud-local-access)

从这些行为出发，我们推导需要持久职责管理、可恢复任务、事件唤醒、模型接口、权限检查、隔离执行、用户接管和通知交付。**这是需求推导，不是发现了 dots 的源代码。**

## 4. Go 与 Python：选择理由和性能边界

### 4.1 三种方案

| 方案 | 适合的阶段与优势 | 主要代价 | 本项目选择 |
| --- | --- | --- | --- |
| FastAPI + Python Runtime | 团队熟悉 Python、快速实验、模型和数据生态便利 | 需仔细管理异步、进程和阻塞依赖；并非自动低性能 | 保留为原技术原型参考 |
| Go API + Python Runtime | API 能单独扩展，保留现成 Python Agent 实现 | 跨语言模型、预算、状态可能出现双重所有权 | 已有成熟 Python Runtime 时可选 |
| Go API + Go Durable Runtime + 隔离计算工具 | 控制与执行合同统一，适合长连接、事件、配额与连接器治理 | 模型及工具适配需维护，Python 生态通过工具桥接 | **新的推荐方案** |

Go 的 goroutine、channel 等机制用于并发服务设计；Temporal Go Workflow 需要用 SDK 的确定性并发与时间原语，不能把普通 goroutine 直接写进可重放逻辑。[Effective Go](https://go.dev/doc/effective_go#concurrency)、[Temporal workflow package](https://pkg.go.dev/go.temporal.io/sdk/workflow)

这些是能力和工程取舍，没有同条件压测，不能宣称 Go 版 QPS、内存或总任务速度提升某个固定倍数。

### 4.2 性能预算

```text
端到端耗时 = 准入 / 排队 + 上下文准备 + 模型等待
           + 工具耗时 + 持久化 / 交付
```

假设一次模型等待 5 秒、其他应用代码 50 毫秒；把应用代码缩到 10 毫秒，耗时从 5.05 秒变为 5.01 秒，下降约 0.8%。这是示意计算，不是实测。

因此 Go 的优先价值是控制层的资源效率、并发结构与部署管理。消费体验还依赖更短上下文、正确的模型档位、及时流式显示、工具并发限制和减少多余模型调用。定时提醒发出时无需让模型再次判断“现在是否该提醒”。

### 4.3 验证方法

使用同硬件、同数据库、同功能与同模型 stub 比较语言方案，记录请求 P95 / P99、CPU、RSS、连接内存、事件延迟和失败恢复。真实模型测试另外记录 TTFT、最终耗时、Token 和费用，不与 stub 压测混算。

建议建立 API / 长连接、编排、真实供应商、浏览器执行四份性能报告。只有前三者确有瓶颈，才按证据增加缓存、独立事件网关或队列；不因 Go 的选择提前拆十几个微服务。

## 5. 国内大众生活助理的产品变化

| 原 v0.1 假设 | 国内个人用户方案 |
| --- | --- |
| 用户先配置模型 Key | 平台提供国内模型和额度，BYOK 为高级可选项 |
| 多 Agent 配置台 | 一个默认助理，对话与事项卡为主 |
| GitHub / 论文监控 | 生活事务、待办、出行与材料整理 |
| 桌面 Web 优先 | 移动 H5 / 微信小程序优先，Web 承担复杂资料和浏览器接管 |
| 美元费用与 Token | 用户看到剩余任务额度 / 处理进度，运营后台用 CNY 成本账本 |
| 飞书 / Slack 作为首选渠道 | 小程序与站内通知，按实际授权增加日历、邮件或短信 |
| 平台接口自行可用即可发布 | 模型效果、渠道许可、隐私与公开服务上线条件共同决定发布 |

消费级护城河需要验证：用户是否愿意持续把事项交给它、结果是否可靠、触达是否及时、成本是否可控、权限是否让人放心。模型和后端语言只是其中的技术条件。

## 6. 国内接入证据与限制

### 6.1 模型

P0 优先评测千问与 DeepSeek，选择满足中文、时间理解、工具合同和数据处理要求的具体型号；不写死“所有系列都有相同能力”。千问官方提供兼容接口及地域配置，DeepSeek 官方提供工具调用合同。[阿里云兼容接口](https://help.aliyun.com/zh/model-studio/compatibility-of-openai-with-dashscope)、[DeepSeek Tool Calls](https://api-docs.deepseek.com/guides/tool_calls/)

“国内供应商”不能自动证明某条调用的数据驻留，仍检查所购服务地域、实际 endpoint、处理条款与日志设置。海外模型不作为国内大众版默认回退目标。语音、图片、OCR 等按具体型号测试和独立计费。

### 6.2 微信

微信开发者主站的订阅消息页面在本次检索中未能打开；腾讯官方产品文档确认需要用户触发授权，一次授权可发送一条服务通知。[腾讯官方订阅消息文档](https://cloud.tencent.com/document/product/1301/103770)

因此规划不依赖无限免费推送或未经确认的长期模板资格。小程序类目、模板和账号实际资质在接入时核验；“保存计划”和“获得通知授权”分成两个步骤。提醒无法推送时显示渠道不可用并提供站内结果 / 日历导出，不显示已送达。

本次没有核实可供此产品通用读取个人微信聊天、操作任意微信群、读取全部订单或直接替用户在所有 App 下单的官方能力。P0 采用用户主动上传截图、文件、文本，以及合法获准的接口；不把这些未确认的深度接入写成产品承诺。

### 6.3 国内公开服务要求

面向境内公众的生成式服务需要按实际服务形态评估适用义务。调用已上线模型不代表应用方自动完成全部要求。[生成式人工智能服务管理暂行办法](https://www.cac.gov.cn/2023-07/13/c_1690898327029107.htm)

生成内容标识需要覆盖用户界面和适用的下载 / 导出，落实方式还需结合文件类型与相关标准。[人工智能生成合成内容标识办法](https://www.cac.gov.cn/2025-03/14/c_1743654684782215.htm)

发布计划要同时验证域名 / 小程序上架要求、服务条款、数据同意与删除、投诉入口、内容安全和所适用的登记 / 备案 / 评估。具体义务由资质、功能与主管要求确认；本设计不作“一律需要某种备案”或“内测即可免除”的判断。

## 7. 对标的发布边界

P0 证明生活事项能持续执行、修改、提醒和生成结果，是基础验证版。没有隔离计算、用户接管、受控委派与主动研究时，不宣传为 dots 全功能等价。

P1 增加上述能力及实时中文语音，在一组允许的网站与生活场景跑通完整闭环，再发布消费级对标内测。P2 再验证本地电脑、更多官方应用、家庭协作与更广的服务生态。

文档中的所有对标阶段都是规划。相同功能名称不代表相同质量、覆盖率、成本或可用规模，应以场景评测与用户留存验证。
