use redis::{Client, Commands, ConnectionInfo, ConnectionAddr, RedisConnectionInfo};
use dotenvy::dotenv;
use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use uuid::Uuid;
use chrono::{DateTime, Utc};
use anyhow::{Result, anyhow};
use log::{info, warn, error, debug};
use env_logger::Env;


// Rust analyzer config constants
const MIN_ABSOLUTE_PROFIT: f64 = 10.0; // Minimum absolute profit in USDT
const MIN_ROI_PERCENTAGE: f64 = 0.5; // Minimum ROI percentage

#[derive(Debug, Clone, Deserialize, Serialize)]
struct OrderBook {
    #[serde(rename = "exchange")]
    exchange: String,
    #[serde(rename = "pair")]
    pair: String,
    #[serde(rename = "bids")]
    bids: Vec<Vec<f64>>, // [[price, size], [price,size]] matching our go codebase
    #[serde(rename = "asks")]
    asks: Vec<Vec<f64>>, // [[price, size], [price,size]] matching our go codebase
    #[serde(rename = "timestamp")]
    timestamp: i64,
}

#[derive(Debug, Clone, Deserialize, Serialize)]
struct ArbitrageOpportunity {
    id: String,
    buy_exchange: String,
    sell_exchange: String,
    pair: String,
    buy_price: f64,
    sell_price: f64,
    max_size: f64,
    gross_profit_per_unit: f64,
    estimated_fees: f64,
    net_profit: f64,
    roi_percentage: f64,
    timestamp: DateTime<Utc>,
}


#[derive(Debug, Clone, Serialize)]
struct ExecutionRequest {
    id: String,
    opportunity: ArbitrageOpportunity,
    execution_size: f64,
    created_at: DateTime<Utc>,
}

#[derive(Debug, Clone)]
struct SpreadAnalyzer {
    books: HashMap<String, OrderBook>,
    redis_client: Client,
    fees_config: FeesConfig,
}

#[derive(Debug, Clone)]
struct FeesConfig {
    // Trading fees as percentage (e.g., 0.1 for 0.1%)
    binance_taker_fee: f64,
    binance_maker_fee: f64,
    uniswap_fee: f64,
    // Gas costs in USD
    ethereum_gas_cost: f64,
    // Withdrawal fees
    withdrawal_fees: HashMap<String, f64>,
    // execution strategy
    use_market_orders: bool, // true = taker fees, false = maker fees
}

impl Default for FeesConfig {
    fn default() -> Self {
        let mut withdrawal_fees: HashMap<String, f64> = HashMap::new();
        withdrawal_fees.insert("BTC".to_string(), 0.0005);
        withdrawal_fees.insert("WBTC".to_string(), 0.0005); // Same as BTC
        withdrawal_fees.insert("ETH".to_string(), 0.005);
        withdrawal_fees.insert("USDT".to_string(), 10.0);

        // This can change. VARIABLE
        FeesConfig {
            binance_taker_fee: 0.1, // 0.1%
            binance_maker_fee: 0.1,
            uniswap_fee: 0.3, // 0.3%
            ethereum_gas_cost: 50.0, // $50 average gas cost
            withdrawal_fees,
            use_market_orders: true, // Default to use taker fees for speed of execution.
        }
    }
}

impl SpreadAnalyzer {
    fn new(_redis_url: &str) -> Result<Self> {
        let addr = std::env::var("REDIS_ADDR").unwrap_or_else(|_| "127.0.0.1:6379".to_string());
        let mut parts = addr.split(':');
        let host = parts.next().unwrap_or("127.0.0.1").to_string();
        let port: u16 = parts.next().and_then(|p| p.parse().ok()).unwrap_or(6379);
        let password = std::env::var("REDIS_PASS").ok();
        let info = ConnectionInfo {
            addr: ConnectionAddr::Tcp(host, port),
            redis: RedisConnectionInfo {
                db: 0,
                username: None,         // or Some(user) if you have REDIS_USER
                password,
                ..Default::default()
            },
        };
        let client = Client::open(info)?;
        Ok(SpreadAnalyzer {
            books: HashMap::new(),
            redis_client: client,
            fees_config: FeesConfig::default(),
        })
    }

    fn parse_key_from_payload(&self, payload: &str) -> Result<String> {
        if let Ok(json_value) = serde_json::from_str::<serde_json::Value>(payload) {
            if let Some(key) = json_value.get("key").and_then(|k| k.as_str()) {
                return Ok(key.to_string());
            }
        }
        Ok(payload.to_string())
    }

    fn estimate_fees_and_gas(&self, size: f64, buy_exchange: &str, sell_exchange: &str, pair: &str) -> f64 {
        /*
            In Arbitrage Context:
            - Taker fees apply when you use market orders (immediate execution)
            - Maker fees apply when you use limit orders (add liquidity to orderbook)


         */
        let mut total_fees: f64 = 0.0;

        // Extract base currency from pair (e.g. WBTC from WBTC/USDT)
        let base_currency = if pair.contains("/") {
            pair.split("/").next().unwrap_or("BTC").to_string()
        } else if pair.contains("USDT") {
            pair.replace("USDT", "")
        } else if pair.contains("USD") {
            pair.replace("USD", "")
        } else {
            // Fallback: Asumming first 3-4 characters for now
            pair.chars().take(4).collect()
        };

        // matching the buy exchanging
        match buy_exchange {
            "binance" => { total_fees += size * self.fees_config.binance_taker_fee / 100.0; }
            "uniswap-v3-exact" => {
                total_fees += size * self.fees_config.uniswap_fee / 100.0;
                total_fees += self.fees_config.ethereum_gas_cost;
            }
            _ => { total_fees += size * 0.15 / 100.0; }
        };

        match sell_exchange {
            "binance" => { total_fees += size * self.fees_config.binance_taker_fee / 100.0; }
            "uniswap-v3-exact" => {
                total_fees += size * self.fees_config.uniswap_fee / 100.0;
                total_fees += self.fees_config.ethereum_gas_cost;
            }
            _ => { total_fees += size * 0.15 / 100.0; }
        }

        // Withdrawal/transfer fees - normalize WBTC to BTC for fee lookup
        let fee_lookup_currency = base_currency.replace("WBTC", "BTC");
        if let Some(withdrawal_fee) = self.fees_config.withdrawal_fees.get(&fee_lookup_currency) {
            total_fees += withdrawal_fee * size; // Assuming withdrawal fee is per unit
        }
        total_fees
    }

    fn normalize_pair_symbols(&self, pair1: &str, pair2: &str) -> (String, String, f64) {
        // Handle token symbol mapping (e.g., WBTC vs BTC)
        // Your Go code uses format like "WBTC/USDT", so we need to handle "/" separator
        let normalized_pair1 = pair1.replace("WBTC", "BTC");
        let normalized_pair2 = pair2.replace("WBTC", "BTC");
        
        // Price adjustment factor for wrapped tokens
        let price_adjustment = if pair1.contains("WBTC") || pair2.contains("WBTC") {
            0.9999 // WBTC typically trades at slight discount to BTC
        } else {
            1.0
        };

        (normalized_pair1, normalized_pair2, price_adjustment)
    }

    fn choose_execution_size(&self, ask_size: f64, bid_size: f64) -> f64 {
        // Take the minimum to ensure we can execute both sides
        let max_possible: f64 = ask_size.min(bid_size);

        // Apply conservative sizing (80% of max possible)
        let conservative_size: f64 = max_possible * 0.8;

        // Cap at reasonable maximum (e.g., $100K possible)
        let max_usd_size: f64 = 100000.0;
        let reasonable_max: f64 = max_usd_size / 50000.0;

        conservative_size.min(reasonable_max)
    }

    fn analyze_spread(&mut self, updated_key: &str) -> Result<Vec<ArbitrageOpportunity>> {
        let mut opportunities: Vec<ArbitrageOpportunity> = Vec::new();

        // Get the updated orderbook
        let updated_book: &OrderBook = self.books.get(updated_key).ok_or_else(|| anyhow!("Orderbook not found for key: {}", updated_key))?;

        debug!("Analyzing spread for updated book: {}", updated_key);

        // Compare with all other books for the same normalized pair
        for (other_key, other_book) in &self.books {
            if other_key == updated_key {
                continue;
            }

            // Normalize pair symbols for comparison
            let (norm_pair1, norm_pair2, price_adjustment) = self.normalize_pair_symbols(&updated_book.pair, &other_book.pair);

            if norm_pair1 != norm_pair2 {
                // Different trading pairs
                continue;
            }

            // Ensure both books have bids and asks
            if updated_book.bids.is_empty() || updated_book.asks.is_empty() || other_book.bids.is_empty() || other_book.asks.is_empty() {
                warn!("Empty orderbook found: {} or {}", updated_key, other_key);
                continue;
            }

            // Scenario 1: Buy from updated_book, sell to other_book
            let buy_price: f64 = updated_book.asks[0][0] * price_adjustment;
            let buy_size: f64 = updated_book.asks[0][1];
            let sell_price: f64 = other_book.bids[0][0];
            let sell_size: f64 = other_book.bids[0][1];

            if let Some(opp) = self.evaluate_opportunity(
                &other_book.exchange,
                &updated_book.exchange,
                &norm_pair1,
                buy_price,
                sell_price,
                buy_size,
                sell_size,
            ) {
                opportunities.push(opp);
            }
        } 

        Ok(opportunities)        
    }

    fn evaluate_opportunity(
        &self,
        buy_exchange: &str,
        sell_exchange: &str,
        pair: &str,
        buy_price: f64,
        sell_price: f64,
        buy_size: f64,
        sell_size: f64,
    ) -> Option<ArbitrageOpportunity> {
        // Check for positive spread
        if sell_price <= buy_price {
            return None;
        }

        let max_size: f64 = self.choose_execution_size(buy_size, sell_size);
        if max_size <= 0.0 {
            return None;
        }

        let gross_profit_per_unit: f64 = sell_price - buy_price;
        let estimated_fees: f64 = self.estimate_fees_and_gas(max_size, buy_exchange, sell_exchange, pair);
        let gross_profit: f64 = gross_profit_per_unit * max_size;
        let net_profit: f64 = gross_profit - estimated_fees;
        let roi_percentage: f64 = (net_profit / (buy_price * max_size)) * 100.0;

        // Check profitability thresholds
        if net_profit < MIN_ABSOLUTE_PROFIT || roi_percentage < MIN_ROI_PERCENTAGE {
            return None;
        }

        Some(ArbitrageOpportunity { 
            id: Uuid::new_v4().to_string(), 
            buy_exchange: buy_exchange.to_string(), 
            sell_exchange: sell_exchange.to_string(), 
            pair: pair.to_string(), 
            buy_price: buy_price, 
            sell_price: sell_price, 
            max_size: max_size, 
            gross_profit_per_unit: gross_profit_per_unit, 
            estimated_fees: estimated_fees, 
            net_profit: net_profit, 
            roi_percentage: roi_percentage, 
            timestamp: Utc::now(),
        })

    }

    fn print_analysis_results(&self, opportunities: &[ArbitrageOpportunity]) {
        if opportunities.is_empty() {
            println!(" SPREAD ANALYSIS: No profitable opportunities found");
            return;
        }

        println!("\n ARBITRAGE OPPORTUNITIES DETECTED ");
        println!("═══════════════════════════════════════════");

        for (idx, opp) in opportunities.iter().enumerate() {
            println!("\n\n Opportunity #{}", idx + 1);
            println!("  ID: {}", opp.id);
            println!("  Strategy: Buy {} → Sell {}", opp.buy_exchange, opp.sell_exchange);
            println!("  Pair: {}", opp.pair);
            println!("  Buy Price: ${:.4}", opp.buy_price);
            println!("  Sell Price: ${:.4}", opp.sell_price);
            println!("  Spread: ${:.4} ({:.3}%)", 
                     opp.gross_profit_per_unit, 
                     (opp.gross_profit_per_unit / opp.buy_price) * 100.0);
            println!("  Max Execution Size: {:.6}", opp.max_size);
            println!("  Gross Profit: ${:.2}", opp.gross_profit_per_unit * opp.max_size);
            println!("  Estimated Fees: ${:.2}", opp.estimated_fees);
            println!("  NET PROFIT: ${:.2}", opp.net_profit);
            println!("  ROI: {:.2}%", opp.roi_percentage);
            println!("  Timestamp: {}", opp.timestamp.format("%Y-%m-%d %H:%M:%S UTC"));
            
            // Risk assessment
            if opp.roi_percentage > 2.0 {
                println!("  Risk Level: HIGH PROFIT");
            } else if opp.roi_percentage > 1.0 {
                println!("  Risk Level: MODERATE");
            } else {
                println!("  Risk Level: LOW MARGIN");
            }
        }
    }

    fn run(&mut self) -> Result<(), anyhow::Error> {
        info!(" Starting Spread Analysis...");

        let mut con = self.redis_client.get_connection()?;
        let mut pubsub = con.as_pubsub();

        pubsub.subscribe("orderbook_updates")?;
        info!("Subscribed to orderbook_updates channel");


        // To keep checking for the updates from the channel from redis
        loop {
            let msg = pubsub.get_message()?;
            let payload: String = msg.get_payload()?;

            debug!("Received message: {}", payload);

            // Parsing the key from the payload
            let key = match self.parse_key_from_payload(&payload) {
                Ok(key) => key,
                Err(e) => {
                    error!("Failed to parse key from payload: {}", e);
                    continue;
                }
            };

            // Fetching the most updated orderbook from redis
            let mut redis_con = self.redis_client.get_connection()?;
            let json_data: String = match redis_con.get(&key) {
                Ok(data) => data,
                Err(e) => {
                    error!("Failed to fetch orderbook : {}", e);
                    continue;
                }
            };

            // parse the orderbook
            let orderbook: OrderBook = match serde_json::from_str(&json_data) {
                Ok(ob) => ob,
                Err(e) => {
                    error!("Failed to parse orderbook JSON for {}: {}", key, e);
                    continue;
                }
            };

            // Store locally in the format as our go codebase: order:exchange:pair
            let book_key = format!("{}:{}", orderbook.exchange, orderbook.pair);
            self.books.insert(book_key.clone(), orderbook.clone());

            info!("Updated orderbook: {} (bids: {}, asks: {})", book_key, orderbook.bids.len(), orderbook.asks.len());

            // Perform spread analysis
            match self.analyze_spread(&book_key) {
                Ok(opportunities) => {
                    self.print_analysis_results(&opportunities);
                    
                    // Here you would publish execution requests to Redis
                    // For now, we just log the intent
                    for opp in opportunities {
                        let exec_request = ExecutionRequest {
                            id: Uuid::new_v4().to_string(),
                            opportunity: opp.clone(),
                            execution_size: opp.max_size,
                            created_at: Utc::now(),
                        };
                        
                        info!("  Would execute: {} (Net: ${:.2}, ROI: {:.2}%)", exec_request.id, opp.net_profit, opp.roi_percentage);
                        
                        // TODO: Publish to execution stream
                        
                    }
                }
                Err(e) => error!("Spread analysis failed: {}", e),
            }            
        }
    }
}


fn main() -> Result<()> {
    // Load environment variables from .env if present
    let _ = dotenv();
    // Initialize logging - set RUST_LOG=debug for verbose output
    // env_logger::init();
    env_logger::Builder::from_env(Env::default().default_filter_or("info")).init();

    
    info!("  Starting Arbitrage Spread Analyzer");
    info!("  Monitoring Redis for orderbook updates...");
    
    // Configuration: log the address we will actually use
    let redis_addr = std::env::var("REDIS_ADDR").unwrap_or_else(|_| "127.0.0.1:6379".to_string());
    info!("  Connecting to Redis at: {}", redis_addr);
    
    // Create and configure the analyzer
    let mut analyzer = SpreadAnalyzer::new(&redis_addr)?;
    
    // Optional: Customize fee configuration
    analyzer.fees_config.use_market_orders = true; // Use taker fees for speed
    analyzer.fees_config.binance_taker_fee = 0.1; // 0.1% for regular users
    analyzer.fees_config.ethereum_gas_cost = 50.0; // Adjust based on current gas prices
    
    info!("   Configuration:");
    info!("   - Execution Strategy: {}", 
          if analyzer.fees_config.use_market_orders { "Market Orders (Taker)" } else { "Limit Orders (Maker)" });
    info!("   - Binance Fee: {:.3}%", 
          if analyzer.fees_config.use_market_orders { 
              analyzer.fees_config.binance_taker_fee 
          } else { 
              analyzer.fees_config.binance_maker_fee 
          });
    info!("   - Uniswap Fee: {:.1}%", analyzer.fees_config.uniswap_fee);
    info!("   - Min Profit: ${:.2}", MIN_ABSOLUTE_PROFIT);
    info!("   - Min ROI: {:.1}%", MIN_ROI_PERCENTAGE);
    
    // Test Redis connection
    match analyzer.redis_client.get_connection() {
        Ok(_) => info!(" Redis connection successful"),
        Err(e) => {
            error!(" Failed to connect to Redis: {}", e);
            error!(" Make sure Redis is running: redis-server");
            return Err(e.into());
        }
    }
    
    info!(" Analyzer ready! Waiting for orderbook updates...");
    info!(" Supported exchanges: binance, uniswap-v3-exact");
    info!(" Press Ctrl+C to stop");
    
    // Run the main analysis loop
    analyzer.run()
}