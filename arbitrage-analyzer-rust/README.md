# Arbitrage Analyzer in Rust

## Overview
The Rust analyzer subscribes to Redis pub/sub updates and evaluates cross-exchange arbitrage opportunities in real time. It ingests up-to-date order books (from your Go feeder or any producer), computes spreads, estimates fees and gas, and prints actionable opportunities with ROI and net profit.

Core entry points and types (see `src/main.rs`):
- `SpreadAnalyzer::run()` subscribes to `orderbook_updates` and performs analysis.
- `SpreadAnalyzer::analyze_spread()` compares the updated book against others for the same pair.
- `SpreadAnalyzer::estimate_fees_and_gas()` estimates taker/maker, AMM, gas, and withdrawal fees.
- `ArbitrageOpportunity` summarizes a detected opportunity.

## Features
- Live Redis subscription to `orderbook_updates`.
- Normalization of symbols (e.g., `WBTC -> BTC`) for pair matching.
- Conservative execution sizing based on top-of-book sizes.
- Fee model with centralized exchange fees, Uniswap v3 fee, ETH gas, and optional withdrawal fees.
- Configurable execution strategy (market/taker vs limit/maker).
- Structured logging with `env_logger` and `.env` loading via `dotenvy`.

## Requirements
- Rust 1.80+ (2024 edition)
- Redis server reachable from the analyzer
- A producer publishing order book data into Redis (../arbitrage-bot-go or your own implementation)

## Project layout
- `src/main.rs` — Analyzer logic and runtime.
- `Cargo.toml` — Dependencies (`redis`, `serde`, `chrono`, `dotenvy`, `env_logger`, `anyhow`, etc.).
- `.env` — Local environment variables (ignored by git).

## Install & Build
```bash
cd arbitrage-analyzer-rust
cargo build
```

## Configuration
Environment variables (loaded via `.env` thanks to `dotenvy`):

- `REDIS_ADDR` — host:port of Redis. Default: `127.0.0.1:6379`.
- `REDIS_PASS` — password for Redis (if required).
- `REDIS_USER` — optional ACL username (if your Redis uses usernames).
- `RUST_LOG` — optional log filter (e.g., `info`, `debug`). The app defaults to `info` if unset.

Example `.env`:
```env
# Redis
REDIS_ADDR=localhost:6379
REDIS_PASS=password
# REDIS_USER=default

# Logging
RUST_LOG=info
```

Note: The analyzer constructs a `redis::ConnectionInfo` directly from `REDIS_ADDR`, `REDIS_PASS`, and optionally `REDIS_USER`. You do not have to provide a URL.

## Running
```bash
cd arbitrage-analyzer-rust
cargo run
```

You should see logs like:
```
[INFO]   Starting Arbitrage Spread Analyzer
[INFO]   Monitoring Redis for orderbook updates...
[INFO]   Connecting to Redis at: localhost:6379
[INFO]   Configuration:
...
[INFO]   Analyzer ready! Waiting for orderbook updates...
```

If you want more verbosity:
```bash
RUST_LOG=debug cargo run
```

## Redis channels and keys
- Subscribes to channel: `orderbook_updates`
  - The message payload can be either:
    - A raw key string, or
    - A JSON object like `{ "key": "exchange:PAIR" }`
- The analyzer then runs `GET <key>` to fetch the latest order book JSON and caches it in-memory under the same key format `exchange:PAIR` (e.g., `binance:WBTC/USDT`).

## Order book JSON format
Matches the Go producer structure:
```json
{
  "exchange": "binance",
  "pair": "WBTC/USDT",
  "bids": [[price, size], [price, size]],
  "asks": [[price, size], [price, size]],
  "timestamp": 1699999999
}
```

Rust struct (for reference): `OrderBook { exchange, pair, bids, asks, timestamp }`.

## How it works
- `SpreadAnalyzer::run()`:
  - Subscribes to `orderbook_updates` via Redis `PubSub`.
  - Parses the key from the message payload.
  - Fetches the latest order book JSON and stores it in `books: HashMap<String, OrderBook>`.
  - Calls `analyze_spread()` to compare the updated book against others of the same normalized pair.

- `analyze_spread()`:
  - Normalizes pairs (e.g., WBTC -> BTC) so `WBTC/USDT` and `BTC/USDT` can be compared.
  - Requires both books to have bids and asks.
  - Considers buying at the best ask of one book and selling at the best bid of the other.
  - Uses `choose_execution_size()` to select a conservative executable size.
  - Calls `evaluate_opportunity()` for profitability checks and thresholds.

- `estimate_fees_and_gas(size, buy_exchange, sell_exchange, pair)`:
  - Centralized exchanges (e.g., `binance`) use configured taker/maker fee percent.
  - Uniswap v3 exact swaps add pool fee percent and an ETH gas USD estimate.
  - Withdrawal fees are looked up by base symbol, normalizing `WBTC -> BTC`.

- `ArbitrageOpportunity`:
  - Contains `buy_exchange`, `sell_exchange`, `pair`, prices, `max_size`, `gross_profit_per_unit`, `estimated_fees`, `net_profit`, `roi_percentage`, and `timestamp`.
  - Printed with spread, gross, fee, net, and ROI details.

## Fee model
- `FeesConfig` (see `src/main.rs`):
  - `binance_taker_fee`, `binance_maker_fee` (percentage, e.g., `0.1` for 0.1%).
  - `uniswap_fee` (percentage, e.g., `0.3` for 0.3%).
  - `ethereum_gas_cost` (USD estimate per swap path).
  - `withdrawal_fees: HashMap<String, f64>` keyed by base asset symbol (e.g., `BTC`, `ETH`, `USDT`).
  - `use_market_orders` toggles taker vs maker assumptions.

Tune these based on market conditions and your account tiers.

## Troubleshooting
- No logs at startup:
  - Ensure `RUST_LOG` is at least `info`, or rely on the built-in default (we set it to `info`).
- `NOAUTH: Authentication required`:
  - Verify `.env` has the correct `REDIS_PASS` (and `REDIS_USER` if required).
  - The analyzer logs a successful connection after auth.
- No opportunities:
  - Confirm the producer is publishing to `orderbook_updates` and writing order books under keys like `exchange:PAIR`.
  - Ensure both books for the normalized pair have bids and asks populated.
- Performance:
  - This sample analyzes top-of-book only. Extend to depth-aware sizing/execution if needed.

## Extending
- Depth-aware sizing and slippage modeling.
- Multi-hop routes and cross-venue settlement costs.
- Risk management and execution throttling.
- Publishing `ExecutionRequest` back to Redis for an executor service.

## License
MIT 