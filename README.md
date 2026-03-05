# Polymarket Signal Autopilot

> **Project Name**: `polymarket-signal-autopilot`
>
> **Description**: Go-based realtime Polymarket signal detection and guarded execution pipeline, with PostgreSQL persistence, risk controls, and operational telemetry.

一个基于 Go 的 Polymarket 实时信号与执行管道：

- 接入 Polymarket REST + WebSocket 实时市场数据
- 生成 rank-shift lead-lag 信号
- 持久化原始事件/信号/订单尝试/通知
- 可选自动下单（私有签名 CLOB 接口）
- 订单状态回查与周期对账
- 提供 worker 运维健康与指标端点

## 在服务器上运行，必须用 Docker 吗？

不是必须。你可以直接在服务器安装 Go + PostgreSQL 后用 `go run`/systemd 运行。

但如果你希望部署一致性更高、迁移更简单、环境隔离更清晰，推荐使用 Docker（尤其是 `docker compose`）。本仓库已提供完整 Docker 化配置。

## 这个项目有什么用

这个项目的定位不是高频撮合系统，而是一个**可观测、可回放、可风控的策略执行骨架**。它适合：

1. 快速把 Polymarket 数据流接进来做信号研究
2. 在风控约束下把信号接到真实下单链路
3. 以数据库留痕方式做审计、故障排查和运行复盘

## 核心能力

- **数据面**：Gamma/CLOB REST + CLOB WS
- **策略面**：`internal/strategy/rank_shift.go`
- **执行面**：私有 CLOB 下单 + 动态 nonce + 动态定价回退
- **风控面**：白名单、单市场敞口、日亏阈值、数据新鲜度、kill-switch
- **运维面**：Telegram 告警、`/health`、`/metrics`、订单回查对账
- **异动面板**：轻微/中等/严重（low/medium/high）分级计数与最近异动列表

## 目录结构（关键）

- `cmd/api`：HTTP API 进程（查询 markets/orderbook/trades）
- `cmd/worker`：实时流处理 + 信号 + 执行 + 告警
- `internal/connectors/polymarket`：Polymarket 客户端和签名下单
- `internal/runtime`：autopilot 主循环、对账、telemetry
- `internal/persistence`：PostgreSQL 存储访问
- `migrations`：数据库迁移 SQL
- `tests`：unit/integration/e2e 测试

## Docker 部署（推荐）

### 1) 准备环境变量

```bash
cp .env.example .env
```

至少需要把 `.env` 里的 `POLY_ASSET_IDS`、`ARB_ASSET_A`、`ARB_ASSET_B` 改成真实 token id。

### 2) 一键启动（Postgres + migrate + api + worker）

```bash
docker compose up -d --build
```

启动后：

- API 健康：`GET http://localhost:8080/health`
- Worker 健康：`GET http://localhost:9091/health`
- Worker 指标：`GET http://localhost:9091/metrics`

### 3) 查看日志

```bash
docker compose logs -f api worker
```

### 4) 停止服务

```bash
docker compose down
```

## 单独构建镜像

```bash
docker build --target api -t polymarket-signal-api:local .
docker build --target worker -t polymarket-signal-worker:local .
```

## GitHub Actions 自动构建镜像

仓库内置工作流：`.github/workflows/docker.yml`

- PR：仅构建（不推送）
- Push 到 `main` / `master` 和 `v*` tag：构建并推送到 GHCR

镜像命名规则：

- `ghcr.io/<owner>/<repo>-api:<tag>`
- `ghcr.io/<owner>/<repo>-worker:<tag>`

例如当前仓库：

```bash
docker pull ghcr.io/xeron2000/polymarket-signal-autopilot-api:main
docker pull ghcr.io/xeron2000/polymarket-signal-autopilot-worker:main
```

## 非 Docker 运行（Go 本地）

### 1) 准备

- Go 1.22+
- PostgreSQL

安装与检查：

```bash
go mod tidy
go build ./...
go test ./...
```

### 2) 执行数据库迁移

按顺序执行 `migrations/001_raw_events.sql` 到 `migrations/005_runtime_state.sql`。

```bash
psql "$DATABASE_URL" -f migrations/001_raw_events.sql
psql "$DATABASE_URL" -f migrations/002_news_events.sql
psql "$DATABASE_URL" -f migrations/003_linked_events.sql
psql "$DATABASE_URL" -f migrations/004_execution_pipeline.sql
psql "$DATABASE_URL" -f migrations/005_runtime_state.sql
```

### 3) 启动 API

```bash
API_ADDR=:8080 \
POLY_GAMMA_URL=https://gamma-api.polymarket.com \
POLY_CLOB_URL=https://clob.polymarket.com \
go run ./cmd/api
```

### 4) 启动 Worker（信号 + 持久化）

```bash
DATABASE_URL=postgres://user:pass@localhost:5432/polysignal?sslmode=disable \
POLY_ASSET_IDS=<TOKEN_ID_1>,<TOKEN_ID_2> \
ARB_ASSET_A=<TOKEN_ID_1> \
ARB_ASSET_B=<TOKEN_ID_2> \
WORKER_OPS_ADDR=:9091 \
go run ./cmd/worker
```

### 5) 开启自动执行（可选）

设置 `AUTO_EXECUTE=true`，并提供 `POLY_TRADER_ADDRESS/POLY_API_KEY/POLY_PASSPHRASE/POLY_API_SECRET` 以及 `POLY_PRIVATE_KEY`。

自动执行模式下，nonce 由数据库 `runtime_state` 做原子分配（跨进程共享同一 DB 时可避免 nonce 竞争）。

完整参数清单见：`docs/runbooks/real-data-quickstart.md`。

## 运维入口

- API 健康：`GET /health`（API 进程）
- Worker 健康：`GET http://<worker-host>:9091/health`
- Worker 指标：`GET http://<worker-host>:9091/metrics`
- 异动分级面板（JSON）：`GET http://<worker-host>:9091/anomaly-panel?limit=20`
- 异动分级面板（HTML）：`GET http://<worker-host>:9091/anomaly-panel.html?limit=20`

## 异动分级参数

- `ANOMALY_SEVERITY_MEDIUM`：中等阈值（默认 `0.08`）
- `ANOMALY_SEVERITY_HIGH`：严重阈值（默认 `0.15`）
- `ANOMALY_ALERT_MIN_SEVERITY`：告警最低等级（`low|medium|high`，默认 `medium`）

## 验证命令

```bash
go test ./...
go test -race ./...
go build ./...
go vet ./...
```
