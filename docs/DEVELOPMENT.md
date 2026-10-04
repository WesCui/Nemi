# Nemi 开发运行指南

这是本机私有开发配置；`APP_ENV` 目前只接受 `development`。邀请口令登录只映射一个本人空间，不能发同一个口令给多个陌生人当作多用户系统。

## 1. Windows 原生依赖

后续开发首选 **Git Bash**。Windows 的 `bash` 命令可能指向 WSL，应打开已安装的 Git Bash，或明确使用 `C:/Program Files/Git/bin/bash.exe`。不需要安装 WSL。

首次 clone 后在 Git Bash 中准备：

```bash
cp .env.example .env
# 编辑 .env，设置 APP_INVITE_CODE，原生库使用 127.0.0.1:55432
# 当前电脑 APP_ORIGIN 使用 http://localhost:3100
npm ci
bash scripts/install-temporal.sh
source scripts/env.sh
```

之后独立终端分别启动：

```bash
npm run db
```

```bash
mkdir -p data
.cache/temporal/temporal.exe server start-dev --ip 127.0.0.1 --db-filename data/temporal.db --ui-port 8233
```

另外四个 Git Bash 终端先 `source scripts/env.sh`，再分别 `go run ./cmd/control-api`、`go run ./cmd/runtime-worker`、`go run ./cmd/notification-worker`、`go run ./cmd/relay`。最后在 `apps/web` 中执行 `npm ci` 和 `npm run dev -- --port 3100`。

也可以在根目录执行 `bash scripts/build.sh`，再从 `.cache/bin` 运行四个二进制；Windows 后缀为 `.exe`。更新版本时同步重启 API 与全部 Worker，Web 重新构建后再启动，避免混用不同 Model Profile。迁移会给现有提醒补上 `repeat=once`，保留原始事项、提醒和结果。

Bash 验证命令：

浏览器测试使用本机已安装的 Google Chrome。先启动原生 PG / Temporal 并准备 nemi_test，构建二进制后用隔离 runner；runner 自行启动测试 API / Web / Worker / Relay，使用随机独立队列和仅编译进 Go 测试二进制的供应商协议替身，结束后停止自己启动的进程。产品二进制不存在模拟生成器。测试端口为 18080 / 3310，Web 缓存为 .next-e2e，不改动用户库。

```bash
source scripts/env.sh
export TEST_DATABASE_URL='postgres://nemi:nemi_dev_only@127.0.0.1:55432/nemi_test?sslmode=disable'
export TEST_TEMPORAL_ADDRESS=127.0.0.1:7233
go test -count=1 ./...
go vet ./...
bash scripts/build.sh
npm run test:e2e:isolated
```

下方 PowerShell 命令保留作可选参考；Bash 环境脚本按字面值读取 `.env`，不会把其中的内容当脚本执行。

## v0.3 应用与界面

高德搜索、单次日历导出和三平台群机器人发送的配置与使用见 [国内应用接入](./INTEGRATIONS.md)。可选凭证只填本地 .env；更改后重启 API。群名必须对应真实接收群，平台规则和实际回执需本人核对。提醒仍留在站内，尚未自动连接外部群。

## 延续的生活助理功能

- **生活偏好**：新增内容、选择场景、确认保存。支持修改和移除；生成清单前可取消“参考已确认的偏好”。仅保存用户确认的内容，不自动从对话提取。
- **持续跟进**：打开事项，使用“补充或修改资料”。保存后可重新整理清单；更新资料不会自动覆盖已有清单。
- **周期提醒**：选择首次日期和每天 / 周一至周五 / 每周频率，确认结束日期。每周沿用首次日期的星期和时间，周一至周五不计算法定调休。停用在提醒设置中操作，不停止模型工作。
- **工作动态**：查看后台进度和近期记录，点击“查看”进入原事项。当前没有模型 Run 的暂停、取消和实时 Token 输出。

提醒记录需要打开页面查看；重复计划不等于周期联网检查或周期模型研究。详细边界见 [实施状态](./IMPLEMENTATION.md)。

在项目根目录执行：

以下保留为 PowerShell 可选参考；后续项目开发实际使用上方的 Git Bash 流程。

```powershell
Copy-Item .env.example .env
# 修改 APP_INVITE_CODE；原生 PostgreSQL 端口使用 55432：
# DATABASE_URL=postgres://nemi:nemi_dev_only@127.0.0.1:55432/nemi?sslmode=disable
npm ci
./scripts/install-temporal.ps1
```

CLI 安装脚本固定 Temporal 1.9.1，并比对官方 release 的 SHA-256。PostgreSQL 开发包固定 17.10，依赖校验写入 package-lock.json；它是项目开发工具，不是生产安装方式。[Temporal 官方发行](https://github.com/temporalio/cli/releases/tag/v1.9.1)、[Embedded Postgres 项目](https://github.com/leinelissen/embedded-postgres)。

打开单独终端启动数据库：

```powershell
npm run db
```

再打开单独终端，启动持久 Temporal 开发服务器：

```powershell
New-Item -ItemType Directory -Path data -Force
.cache/temporal/temporal.exe server start-dev --ip 127.0.0.1 --db-filename data/temporal.db --ui-port 8233
```

## 2. 后端与 Web

以下入口各在独立终端中运行，工作目录均为仓库根目录。API 首次启动会执行幂等 schema 迁移并初始化本地身份；Worker 在 API 初始化成功后启动。

```powershell
. ./scripts/env.ps1
go run ./cmd/control-api
```

其余三个终端分别执行：

```powershell
. ./scripts/env.ps1
go run ./cmd/runtime-worker
```

```powershell
. ./scripts/env.ps1
go run ./cmd/notification-worker
```

```powershell
. ./scripts/env.ps1
go run ./cmd/relay
```

Web 终端：

```powershell
cd apps/web
npm ci
npm run dev
```

访问 http://localhost:3000，输入 `.env` 中的邀请口令。`APP_ORIGIN` 必须与访问来源精确匹配；默认 `http://localhost:3000`，不要混用 `127.0.0.1`。

## 3. 模型配置

默认不设置服务端模型，用户在首页配置自己的服务商、型号、API Key 和单价。无可用配置时，API 明确拒绝创建对话或模型运行；不会生成替代内容。

也可配置实际服务端模型作为可选默认：`qwen`、`deepseek`、`kimi`、`doubao` 或 `glm`、获准的 `MODEL_NAME`、服务端 `MODEL_API_KEY`，并填写该型号当前价格。每百万 Token 的价格转换成 micro-CNY，例如 **仅作换算示例** ￥2 / 百万 Token = `2000000`，不是任何型号现价。密钥不回显、不写日志、不进入 Temporal payload、不提交 Git。

每次 Run 使用固定 provider / model / 提示合同 Profile；运行中更改服务端型号会使旧 Run 明确失败。v0.5 支持普通文本多轮对话和结构化清单；每轮一次模型调用，没有图片、通用工具循环、搜索或供应商回退。具体型号能力及输出质量须用实际账号核验。

请求前预留费用：单 Run 上限 ￥1、个人北京时间自然日上限 ￥3、全局 5 / 每空间 2 个活跃模型工作。预算记录以整数 micro-CNY 存储。真实费用取供应商 usage；缺失 usage、网络响应丢失或 Activity 中断视为费用不确定，保留预留待后续核对。v0.1 不提供自动账单核对器。

## 4. 测试

```powershell
. ./scripts/env.ps1
$env:TEST_DATABASE_URL='postgres://nemi:nemi_dev_only@127.0.0.1:55432/nemi_test?sslmode=disable'
go test -count=1 ./...
go vet ./...
```

测试要求独立 `nemi_test` 数据库，不执行 DROP / TRUNCATE，不碰个人空间的真实数据。集成测试创建独立测试空间，保留在测试库以便核对。重新跑会产生新的测试记录。

```powershell
npx playwright install chromium
$env:NEMI_TEST_INVITE_CODE=$env:APP_INVITE_CODE
npm run test:e2e
```

浏览器测试需所有服务已运行，默认访问 `http://localhost:3000`；通过 `NEMI_BASE_URL` 可选择其他本地端口。测试在当前个人开发空间建立带“验收”前缀的事项并标记完成，切勿对公开部署运行。

## 5. 恢复验证

保存一个未来提醒后，可以停止 notification-worker，过提醒时间再重启。Temporal 保存计时和待执行 Activity，站内记录最终出现一次。超过 10 分钟才恢复时显示“过期提醒”，不声称正常准时触达。

生成清单时关闭网页不会停止 Worker。在实际付费请求中途强制终止 Worker，第三方结果可能未知；本版优先不重复收费，Run 显示未完成，不能声称自动精确续接供应商调用。

## 6. 停止服务

原生开发的终端使用 Ctrl+C。数据保留在 `data`。Docker 使用 `docker compose down`；保留 volumes，不加 `-v`，除非你明确要删除开发数据。

本机工具终端曾出现退出后 Windows 子进程仍存活的情况。若端口仍占用，用 `netstat.exe -ano` 和 `ps -W` 核对 PID、程序路径与启动时间，只停止对应项目进程；不要按 `node.exe` 名称批量结束其他应用。数据库和 Temporal 可以保持运行。

## 7. 已知限制

- 邀请口令、明文开发库、缺少生产身份生命周期治理：只供本机个人验证。
- API、Worker、数据库与 Temporal 分进程，但没有高可用、生产备份、压测或发布验收。
- 没有上传 / S3 / 文档提取，没有微信、飞书或企微真实凭据，也没有手机后台提醒。
- 业务结果在 PostgreSQL 保存；当前不支持机密文件、生产数据加密与完整删除保留流程。
- 单次清单生成只提供规划建议，日期只能经用户明确选择与确认保存。


## v0.4 模型与应用配置

浏览器隔离 runner 每次创建独立测试身份与工作空间，限定 Outbox 只处理该工作空间；使用独立主密钥与随机 Temporal 队列。后端全量测试和浏览器测试应串行执行，避免共用 nemi_test 时的全局费用 / 并发配额互相干扰；不截断任何数据库。

从首页管理个人模型，从连接应用配置三个群机器人和飞书文档自建应用。默认加密主密钥位于被忽略的 data/credentials.key；API 和运行 Worker 从同一个仓库根目录启动。也可设置 APP_CREDENTIAL_KEY（32 字节，64 位 hex），所有进程必须一致。丢失主密钥无法解密数据库中的凭据，不得自动覆盖或重新生成已存在的无效文件；应在页面重新配置。主密钥与数据库分别备份并控制文件 ACL。Compose 已设置共享 credential-data 卷。

保存配置不会发起外部请求。仅对你拥有权限的模型、群与 docx 文档使用页面中的检查 / 确认操作；不要把凭据粘贴到聊天中。真实回执与服务商账单需要实际账号核验。

## v0.6 Agent 开发与联调

主入口为真实模型对话与 Eino 工具执行。未配置个人 / 服务端 Key 时不运行模型；手动保存用户事项仍可使用。个人模型需要支持 OpenAI 兼容 function tool_calls，参数与输出按实际官方服务合同检查。模型使用量、应用凭据与用户数据不要写入测试输出。

浏览器隔离 runner 会编译当前版本的 API、提醒、Relay 与测试专用 Agent Worker 到 .cache/bin/e2e，避免混用旧二进制。它只使用 nemi_test、随机工作空间和独立 Temporal 队列，测试结束关闭自己启动的服务。协议替身只在 Go 测试二进制内，真实产品 Worker 没有生成回退。可用 npm run test:e2e:isolated -- tests/e2e/chat.spec.ts 复验对话。

事务迁移完成 schema v7 后，重启不重复执行 DDL，避免持久任务结算与迁移表锁互相阻塞。改动数据库合同必须提升 schema 版本。Agent 生命周期继续由 Temporal 管理，付费步骤不得通过 Activity / SDK 自动重试；数据库结算可以按同一 position 幂等重试。
