# Real-Time Dynamic Pricing Engine for E-Commerce

Event-driven dynamic pricing platform with Go, Kafka, Redis, Oracle, and a Next.js dashboard.  
The system consumes product interaction events, computes demand-weighted prices in near real time, stores live state in Redis, persists history in Oracle, and streams updates to the UI over WebSockets.

## Core Documents

- [SETUP.md](SETUP.md): detailed setup, run order, and troubleshooting guide for local development.
- [PRD.md](PRD.md): product requirements, scope, and target end-state for the project.

## What Is Implemented

- Local Dockerized infra: Kafka, Zookeeper, Redis, Oracle 23c
- Go services:
  - `event-producer` (simulated events)
  - `pricing-engine` (consume, compute, persist, publish)
  - `api-server` (REST + WebSocket stream + override/history APIs)
- Next.js frontend dashboard:
  - Live price updates via WebSocket (`/ws/prices`)
  - Manual price override set/remove
  - Product history chart
- Static API-key auth for sensitive routes (`override` and `history`)

## Architecture

1. `event-producer` publishes events to Kafka topic `product-events`.
2. `pricing-engine` consumes events and:
   - updates demand in Redis
   - computes new dynamic price
   - writes latest price in Redis
   - inserts Oracle `price_history`
   - publishes `price-updates` event
3. `api-server`:
   - serves `GET /price/{productID}`
   - serves protected override/history APIs
   - consumes `price-updates` and broadcasts WebSocket events on `/ws/prices`
4. Frontend subscribes to WebSocket updates and refreshes only changed products/history.

## Dynamic Price Calculation

The pricing engine updates demand and computes price per consumed event:

- Demand weights:
  - `view = 1`
  - `cart = 3`
  - `purchase = 5`
- Formula:

```text
new_price = event_price + sqrt(current_demand) * 5
```

Where:

- `current_demand` is the weighted demand after applying the incoming event.
- `event_price` comes from the incoming event payload.
- In the current demo producer, `event_price` is a stable catalog base price per product (`PA` to `PJ`), so dynamic movement is driven mainly by demand growth.

## Repository Layout

```text
backend/
  cmd/
    event-producer/
    pricing-engine/
    api-server/
  internal/
    config/
    db/
frontend/
oracle-db-scripts/
tools/
docker-compose.yml
```

## Prerequisites

- Go 1.24+
- Node.js 20+ and npm
- Docker Desktop
- Oracle Instant Client (for `godror` on macOS)

## Quick Start

1. Copy environment template:

```bash
cp .env.example .env
```

2. Start infrastructure:

```bash
docker compose up -d
```

3. Create Kafka topics:

```bash
go run ./tools/create_topics.go
```

4. Start backend services (separate terminals):

```bash
go run ./backend/cmd/pricing-engine
go run ./backend/cmd/api-server
go run ./backend/cmd/event-producer
```

5. Start frontend:

```bash
cd frontend
npm install
npm run dev
```

Open `http://localhost:3000`.

Detailed setup and troubleshooting are in [SETUP.md](SETUP.md).

## Environment Variables

### Backend (`.env` at repo root)

| Variable | Default | Notes |
|---|---|---|
| `KAFKA_BROKERS` | `localhost:9092` | Comma-separated brokers |
| `REDIS_ADDR` | `localhost:6379` | Redis host:port |
| `PRODUCT_EVENTS_TOPIC` | `product-events` | Input topic |
| `PRICE_UPDATES_TOPIC` | `price-updates` | Output/fanout topic |
| `API_PORT` | `8080` | API server port |
| `ADMIN_API_KEY` | none | Required for API startup |
| `API_KEY_HEADER` | `X-API-Key` | Header checked on protected routes |
| `ORACLE_DSN` | `pricing/pricingpass@localhost:1521/FREEPDB1` | Oracle DSN |
| `ORACLE_LIB_DIR` | empty | Optional Oracle Instant Client path |
| `ORACLE_TIMEZONE` | `UTC` | Recommended to avoid timezone warnings |

### Frontend (`frontend/.env.local`)

| Variable | Default | Notes |
|---|---|---|
| `BACKEND_BASE_URL` | `http://localhost:8080` | Next.js proxy target |
| `BACKEND_API_KEY_HEADER` | `X-API-Key` | Must match backend header |
| `NEXT_PUBLIC_PRICE_WS_URL` | `ws://localhost:8080/ws/prices` | Browser WS endpoint |

Use [frontend/.env.example](frontend/.env.example) as template.

## API Summary

### Public

- `GET /price/{productID}`
- `GET /ws/prices` (WebSocket)

### Protected (requires API key header)

- `POST /override`
- `DELETE /override/{productID}`
- `GET /history/{productID}?limit=N`

### Price Response

```json
{
  "product_id": "PA",
  "price": 149.99,
  "demand": 12,
  "price_source": "dynamic"
}
```

### WebSocket Event (`/ws/prices`)

```json
{
  "type": "price_update",
  "product_id": "PA",
  "new_price": 151.34,
  "timestamp": 1773713139
}
```

## Testing

From repo root:

```bash
go test ./...
```

Frontend checks:

```bash
cd frontend
npm run lint
npm run build
```

Optional WebSocket integration smoke test (services must already be running):

```bash
ADMIN_API_KEY=local-admin-key go test -tags=integration ./backend/integration -run TestWebSocketPriceUpdateSmoke -count=1
```

## Known Limitations

- Static admin API key (no user/role model yet)
- Local/demo-oriented deployment only (no Kubernetes/Terraform in current implementation)
- WebSocket fanout is currently single-instance local MVP behavior
- Event producer is synthetic and uses static demo catalog products (`PA` to `PJ`)

## Roadmap (Next Phases)

- Phase 7: testability + packaging (expanded tests, Dockerfiles, CI pipeline)
- Phase 8: infra track (Kubernetes + Terraform baseline)
