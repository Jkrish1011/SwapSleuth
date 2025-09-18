# Arbitrage Bot Connector (Go)

## Overview
This Go service connects to multiple exchanges across chains (e.g., centralized exchanges and AMMs) to fetch live order book data and publishes normalized updates to Redis. It is designed to work alongside the Rust-based analyzer, which subscribes to Redis and detects arbitrage opportunities.

Core responsibilities:
- Connect to supported exchanges (e.g., Binance and Uniswap v3).
- Fetch top-of-book bids/asks (or swap quotes for AMMs).
- Normalize symbol and pair formats.
- Publish order book updates into Redis pub/sub and store current snapshots in Redis keys.

Related project:
- `../arbitrage-analyzer-rust` — Consumes Redis updates and performs spread analysis.

## Features
- Binance order book connector (CeFi).
- Uniswap v3 quotes connector (DeFi) via RPC provider.
- Redis integration: `PUBLISH` on a channel for updates, plus `SET` order book JSON snapshots.
- Configurable via environment variables.
- Structured logging.

## Project Layout
- `main.go` — Entrypoint to start connectors and test Redis connectivity.
- `connectors/`
  - `binance.go` — Binance connector (taker-side order book).
  - `uniswap.go` — Uniswap v3 connector.
- `utils/`
  - `redisConnector.go` — Redis client helpers (publish, set/get, ping).

Note: File names may evolve; check the repository for the latest structure.

## Requirements
- Go 1.21+
- Redis instance reachable from this service
- For Uniswap: an Ethereum RPC endpoint (e.g., Infura, Alchemy) if required by your implementation.

## Installation
```bash
# From repository root
cd arbitrage-bot-go
go mod tidy
```

## Configuration
Environment variables (create a `.env` or export them in your shell):

- `REDIS_ADDR` — Redis address (host:port). Example: `localhost:6379`.
- `REDIS_PASS` — Redis password (optional if Redis has no auth).
- `REDIS_USER` — Redis ACL username (optional).
- `SUBGRAPH_API_KEY` — Optional, only if your implementation uses authenticated endpoints.
- `BINANCE_API_SECRET` — Optional.
- `LOG_LEVEL` — `debug`, `info`, `warn`, `error` (implementation-dependent).

Example `.env`:
```env
REDIS_ADDR=localhost:6379
REDIS_PASS=password
# REDIS_USER=default

# DeFi
ETH_RPC_URL=https://mainnet.infura.io/v3/<your-key>

# CeFi (if needed)
# BINANCE_API_KEY=...
# BINANCE_API_SECRET=...

LOG_LEVEL=info
```

## Build & Run
```bash
go build
./arbitrage-bot-go

# Or directly
go run ./...
```

On startup, you should see logs confirming Redis connectivity and connector initialization.

## Redis Integration
- Channel for updates: `orderbook_updates`
  - Message payload can be either a plain key string or a JSON object like:
    ```json
    { "key": "exchange:PAIR" }
    ```
- Order book snapshot key: `exchange:PAIR`
  - Example keys:
    - `binance:WBTC/USDT`
    - `uniswap-v3-exact:WBTC/USDT`

### Order Book JSON Format
Aligns with the Rust analyzer’s `OrderBook` struct:
```json
{
  "exchange": "binance",
  "pair": "WBTC/USDT",
  "bids": [[price, size], [price, size]],
  "asks": [[price, size], [price, size]],
  "timestamp": 1699999999
}
```

Notes:
- For AMMs like Uniswap v3, you may derive synthetic top-of-book from swap quotes and pool fees.
- Ensure numerical fields are `float64`-compatible and arrays are well-formed.

## How It Works (High Level)
1. Initialize Redis connection.
2. Start connectors:
   - Binance: fetch/order book snapshots, normalize pairs, `SET` to Redis, then `PUBLISH` key on `orderbook_updates`.
   - Uniswap v3: fetch quotes (or pool state), synthesize bid/ask, then `SET`/`PUBLISH` similarly.
3. Repeat at a configured interval or on websocket updates (implementation-dependent).

## Development
- Ensure your connectors return consistent pair notation (e.g., `WBTC/USDT`).
- Keep exchange-specific logic self-contained in `connectors/*`.
- Add robust error handling and retries for network calls.
- Consider rate limiting for centralized exchanges.

## Troubleshooting
- No messages in Redis:
  - Verify `REDIS_ADDR`, password/user, and that Redis is reachable.
  - Use `redis-cli` to `SUBSCRIBE orderbook_updates` and watch messages.
- Rust analyzer not seeing updates:
  - Ensure keys follow `exchange:PAIR` format.
  - Confirm order book JSON structure matches the analyzer’s expectations.
- Uniswap connector issues:
  - Check `ETH_RPC_URL` and network connectivity.
  - Validate pool addresses and fee tiers if you added them.

## Roadmap / Ideas
- Add more CEX connectors (OKX, Bybit, Coinbase Advanced, etc.).
- Extend Uniswap to support multiple fee tiers and multi-hop routing.
- Implement websocket-based streaming for low-latency updates.
- Publish signed execution intents to a trade executor.

## License
MIT