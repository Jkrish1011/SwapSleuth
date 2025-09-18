# SwapSleuth

Real-time cross-exchange crypto arbitrage framework.

SwapSleuth consists of two services that work together:
- `arbitrage-bot-go/` — Go-based exchange connectors that fetch order books/quotes and publish updates to Redis.
- `arbitrage-analyzer-rust/` — Rust-based analyzer that subscribes to Redis, computes spreads, estimates fees/gas, and prints actionable opportunities.

---

## Architecture
- **Producer (Go)**
  - Connects to CEX/DEX venues (e.g., Binance, Uniswap v3).
  - Normalizes symbol pairs (e.g., `WBTC/USDT`).
  - Stores current order books in Redis keys `exchange:PAIR`.
  - Publishes update notifications to the `orderbook_updates` channel.

- **Consumer (Rust)**
  - Subscribes to `orderbook_updates`.
  - Fetches the referenced order book JSON via Redis `GET`.
  - Compares across venues for the same (normalized) pair.
  - Estimates fees/gas and prints net-profit opportunities with ROI.

Data flow:
```
Exchanges -> Go connectors -> Redis (SET + PUBLISH) -> Rust analyzer (SUB -> GET -> analyze)
```

---

## Prerequisites
- Redis instance reachable by both services
- Go 1.21+
- Rust toolchain (2021 edition, Rust 1.70+ recommended)
- Optional: Ethereum RPC endpoint (for Uniswap)

---

## Quick Start
1) Start Redis locally (example):
```bash
redis-server
```

2) Configure environment
- Create `.env` files in each service directory. See the service READMEs for details.
  - `arbitrage-bot-go/.env` (example):
    ```env
    REDIS_ADDR=localhost:6379
    REDIS_PASS=password
    # REDIS_USER=default
    LOG_LEVEL=info
    ```
  - `arbitrage-analyzer-rust/.env` (example):
    ```env
    REDIS_ADDR=localhost:6379
    REDIS_PASS=password
    RUST_LOG=info
    ```

3) Start the analyzer (Rust)
```bash
cd arbitrage-analyzer-rust
cargo run
```
You should see startup logs, successful Redis connection, and a subscription to `orderbook_updates`.

4) Start the connectors (Go)
```bash
cd ../arbitrage-bot-go
go run ./...
```
On success, the connectors will write order book snapshots to Redis keys and publish update messages that the analyzer consumes.

---

## Configuration Summary
Environment variables are loaded from `.env` (see service READMEs for full details).

Common:
- `REDIS_ADDR` — host:port of Redis (e.g., `localhost:6379`).
- `REDIS_PASS` — Redis password (if enabled).
- `REDIS_USER` — Redis ACL username (optional).

Analyzer (Rust):
- `RUST_LOG` — log level (`info`, `debug`, etc.). Defaults to `info` in code if unset.

Connectors (Go):
- `BINANCE_API_KEY`, `BINANCE_API_SECRET` — if using authenticated endpoints.
- `LOG_LEVEL` — connector log level.

---

## Redis Schema
- Channel: `orderbook_updates`
  - Message payload: either a raw key string or JSON `{ "key": "exchange:PAIR" }`.
- Keys: `exchange:PAIR` (e.g., `binance:WBTC/USDT`, `uniswap-v3-exact:WBTC/USDT`).
- Value: order book JSON snapshot.

Order book JSON example:
```json
{
  "exchange": "binance",
  "pair": "WBTC/USDT",
  "bids": [[price, size], [price, size]],
  "asks": [[price, size], [price, size]],
  "timestamp": 1699999999
}
```

---

## How Analysis Works (Rust)
- On each update, the analyzer fetches the latest order book and caches it in memory.
- It normalizes symbols (e.g., `WBTC -> BTC`) to match venues.
- Compares best ask of one venue vs best bid of another venue.
- Chooses a conservative executable size based on top-of-book sizes.
- Estimates fees and costs (exchange fee %, Uniswap pool fee %, ETH gas, optional withdrawals).
- Applies profitability thresholds (min absolute net profit and ROI %).
- Prints opportunities with spread, fees, net, and ROI.

---

## Build & Run (per service)
- Go connectors: see `arbitrage-bot-go/README.md`.
- Rust analyzer: see `arbitrage-analyzer-rust/README.md`.

---

## Troubleshooting
- **No logs in Rust analyzer**: ensure `RUST_LOG=info` (or higher), or use the built-in default (already set to `info`).
- **NOAUTH in Redis**: verify `REDIS_PASS` (and `REDIS_USER` if applicable) in both services.
- **Analyzer shows no opportunities**:
  - Confirm connectors publish to `orderbook_updates` and write snapshots.
  - Ensure keys follow `exchange:PAIR` format and order book JSON matches the schema.
  - Verify both sides have non-empty bids/asks.
- **Uniswap issues**: check `ETH_RPC_URL` and network access.

---

## Roadmap
- Additional CEX connectors (OKX, Bybit, Coinbase Advanced, etc.).
- Deeper DEX integration (multi-hop, multiple fee tiers, better gas modeling).
- Depth-aware sizing and slippage/risk modeling.
- Execution pipeline (publish `ExecutionRequest` to an executor).

---

## License
MIT