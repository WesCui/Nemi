# Nemi · 妮米

**事项、提醒与常用应用，你的个人生活空间。**

面向国内个人用户的持续生活助理，参考 OpenAI dots 的后台执行与持续跟进体验。采用 **Next.js + Go + Temporal + PostgreSQL**，独立实现运行时；dots 的内部技术栈没有公开确认。

## v0.4 已交付

- 中文响应式首页、事项清单、工作动态、生活偏好与应用目录；首页直接配置个人模型。
- 通义千问、DeepSeek、Kimi、豆包、智谱：保存个人密钥、选择默认模型或任务模型、撤销配置、确认后真实调用检查。具体型号需有账号权限并通过检查。
- 凭据使用 AES-256-GCM 加密，绑定空间、用途与配置。密钥不回显，不写入任务参数、业务事件或 Git。
- 飞书、企业微信、钉钉群机器人：页面配置、签名、正文与目标预览、确认发送、持久去重和联调回执。
- 飞书官方 Go SDK：自建应用认证、读取选定且授权的 docx 文档、导入事项资料并保留原文入口。
- 高德官方地点搜索跳转、单次 ICS 日历导出、Markdown 清单下载。
- 事项持续补充资料与版本保护、已确认偏好引用、后台清单生成、费用预留与结算。
- Temporal 持久运行、PG Outbox、共享 SSE；一次性和有结束日期的周期站内提醒、北京时间与免打扰。

保存配置不会自动调用模型、读取文档或发送消息。默认仍可使用明确标识的演示生成器。真实账号授权与实群验收记录见 [实施状态](docs/IMPLEMENTATION.md)。

## 本地运行

Windows 请使用 Git Bash。完整步骤见 [开发运行指南](docs/DEVELOPMENT.md)。

```bash
cp .env.example .env
# 修改邀请口令后启动基础设施与后端：
docker compose up -d --build
cd apps/web
npm ci
npm run dev
```

访问 http://localhost:3000。API 与 Worker 必须共享加密主密钥；Compose 挂载同一 credential-data 卷。无 Docker 时可使用仓库的 PostgreSQL 与 Temporal 开发脚本。

## 验证

所有集成测试使用专用 nemi_test 数据库及独立 Temporal 队列，不能对用户数据库执行清理。先按运行指南配置 TEST_DATABASE_URL 和 TEST_TEMPORAL_ADDRESS，再执行：

```bash
source scripts/env.sh
go test -count=1 ./...
go vet ./...
bash scripts/build.sh
npm run test:e2e:isolated
cd apps/web
npm run build
npm run typecheck
```

## 产品与工程文档

| 文档 | 内容 |
| --- | --- |
| [PRD](docs/PRD.md) | 国内个人生活助理需求与交付范围 |
| [架构](docs/ARCHITECTURE.md) | Go 持久运行、权限、费用与凭据合同 |
| [对标功能状态](docs/PARITY.md) | dots 的公开能力、Nemi 已实现项及尚待交付项 |
| [国内应用接入](docs/INTEGRATIONS.md) | 平台配置、具体场景与授权方法 |
| [实施状态](docs/IMPLEMENTATION.md) | 验收证据与实际边界 |
| [界面设计](docs/DESIGN.md) | 视觉与操作规则 |
| [开源复用](docs/OPEN_SOURCE.md) | Temporal、飞书 SDK、日历组件与许可证 |

当前登录仍采用邀请口令与单个人空间；公开多用户身份、微信小程序、手机通知、语音、云端浏览器、通用工具循环和文件处理尚待开发。本站提醒需要打开页面查看。品牌名 Nemi / 妮米的商标和域名尚未核验。
