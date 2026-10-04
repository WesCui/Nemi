# Nemi · 妮米

**生活里的大小事，有妮米一起惦记。**

面向国内个人用户的持续生活助理。参考 OpenAI dots 的持续职责与后台执行体验，工程底座采用 **Next.js + Go + Temporal + PostgreSQL**。Nemi 是独立实现，dots 的内部技术栈没有公开确认。

当前为 **v0.1 首个可运行切片**，用于私有本地开发。默认使用明确标识的演示生成器，不需要真实模型密钥。它还不是完整 P0，也不适合直接公开部署。

## 已实现

- 个人空间邀请口令登录、服务端会话与数据范围校验。
- 文字资料 → 确认事项、截止时间与提醒 → 保存 → 后台生成行动清单。
- 清单勾选、事项完成与重新跟进、Markdown 清单下载。
- Temporal 模型工作流与独立提醒队列；一次性提醒、北京时间、22:00–08:00 免打扰预览。
- 提醒改期 / 停用、旧 revision 准入检查、站内记录去重；完成事项自动停用提醒。
- PostgreSQL 事务 Outbox、命令幂等、乐观版本检查、持久业务事件与共享 SSE 读取。
- 千问 / DeepSeek 文本清单 Adapter、整数费用预留与结算、并发准入；真实请求结果不确定时保留预留且不自动重发。
- 响应式中文 Web 页面、演示模式提示、飞书 / 企微能力规划入口。

## 本地运行：Docker

需要 Go 1.26、Node.js 22 和已运行的 Docker Desktop / Docker Engine。

```bash
cp .env.example .env
# 修改 .env 中的 APP_INVITE_CODE，随后：
docker compose up -d --build
cd apps/web
npm ci
npm run dev
```

打开 **http://localhost:3000**，输入 `.env` 中的邀请口令。Temporal 开发界面位于 http://localhost:8233，仅监听本机。

本地基础设施只绑定 loopback；`compose.yaml` 中的数据库密码属于开发配置。Temporal 使用持久文件的开发服务器；生产需要独立引擎数据库、认证与备份，不能沿用此配置。

## Windows 无 Docker 开发

提供项目目录内的 PostgreSQL 开发依赖与 Temporal 官方 CLI。安装包和数据分别位于被 Git 忽略的 `.cache`、`data`；不会创建系统服务或系统用户。完整步骤见 [开发运行指南](docs/DEVELOPMENT.md)。

## 验证

```bash
source scripts/env.sh
go test ./...
go vet ./...
cd apps/web
npm run build
npm run typecheck
```

数据库集成测试需单独的 `nemi_test` 数据库和 `TEST_DATABASE_URL`；没有配置时会明确跳过，不能视为集成验证通过。浏览器测试见运行指南。实际本次验证记录见 [实施状态](docs/IMPLEMENTATION.md)。

## 范围与下一步

此切片每次 Run 只调用一次模型生成清单，尚无通用工具循环、浏览器执行或外部自动行动。站内记录需要打开页面查看，不等于手机离线推送。

真实模型账号评测、多用户生产身份、微信小程序 / 订阅通知、文件上传 / OCR / S3、可控记忆、隐私导出删除、飞书 / 企微真实接入、生产加密和部署仍待实施。模型密钥只允许由管理员配置到服务端，不能提交进 Git。

## 设计与调研

| 文档 | 内容 |
| --- | --- |
| [PRD v0.3](docs/PRD.md) | 国内个人生活助理的完整需求与分期 |
| [架构 v0.3](docs/ARCHITECTURE.md) | 目标架构、持久执行、权限、成本与提醒合同 |
| [dots 对标](docs/DOTS_BENCHMARK.md) | 公开能力、未知项与 Go 技术选择 |
| [场景研究](docs/SCENARIO_RESEARCH.md) | 十四个候选场景、飞书与企微权限边界 |
| [实施状态](docs/IMPLEMENTATION.md) | 设计到代码的完成范围与验证证据 |
| [v0.1 设计存档](docs/archive/v0.1/PRD.md) | 早期开发者平台方案，仅供历史参考 |

品牌名 **Nemi / 妮米** 已用于本项目；商标及域名尚未核验。
