# Polymarket Signal Autopilot

Go service for realtime Polymarket signal detection and guarded execution.
It includes PostgreSQL persistence, risk controls, and operations endpoints.

## Do I need Docker on a server?

No. You can run it directly with Go + PostgreSQL.
Docker is recommended for easier deployment and consistent environments.

## Quick start (Docker, recommended)

```bash
cp .env.example .env
```

Set real token IDs in `.env`:

- `POLY_ASSET_IDS`
- `ARB_ASSET_A`
- `ARB_ASSET_B`

Start everything (Postgres + migrations + API + worker):

```bash
docker compose up -d --build
```

Health checks:

- API: `http://localhost:8080/health`
- Worker: `http://localhost:9091/health`
- Metrics: `http://localhost:9091/metrics`

Useful commands:

```bash
docker compose logs -f api worker
docker compose down
```

## Prebuilt images (GHCR)

```bash
docker pull ghcr.io/xeron2000/polymarket-signal-autopilot-api:main
docker pull ghcr.io/xeron2000/polymarket-signal-autopilot-worker:main
```

## Run without Docker

Requirements:

- Go 1.22+
- PostgreSQL

Apply migrations:

```bash
for f in migrations/*.sql; do psql "$DATABASE_URL" -f "$f"; done
```

Run API:

```bash
API_ADDR=:8080 \
POLY_GAMMA_URL=https://gamma-api.polymarket.com \
POLY_CLOB_URL=https://clob.polymarket.com \
go run ./cmd/api
```

Run worker:

```bash
DATABASE_URL=postgres://user:pass@localhost:5432/polysignal?sslmode=disable \
POLY_ASSET_IDS=<TOKEN_ID_1>,<TOKEN_ID_2> \
ARB_ASSET_A=<TOKEN_ID_1> \
ARB_ASSET_B=<TOKEN_ID_2> \
WORKER_OPS_ADDR=:9091 \
go run ./cmd/worker
```

Minimum required worker env:

- `DATABASE_URL`
- `POLY_ASSET_IDS`
- `ARB_ASSET_B` (or at least two values in `POLY_ASSET_IDS`)

For full env list (Telegram, auto-execution, pricing controls), see:
`docs/runbooks/real-data-quickstart.md`

## Verify locally

```bash
go test ./...
go test -race ./...
go build ./...
go vet ./...
```

## License

MIT © Xeron
