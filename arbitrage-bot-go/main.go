package main

import (
	"context"
	"log"

	"github.com/Jkrish1011/SwapSleuth/arbitrage-bot-go/connectors"
	"github.com/Jkrish1011/SwapSleuth/arbitrage-bot-go/utils"
	"github.com/joho/godotenv"
)

func main() {

	err := godotenv.Load()
	if err != nil {
		log.Println("No .env loaded (continuing)")
	}

	// Initialize Redis connection
	log.Println("Initializing Redis connection...")
	utils.InitRedis()

	// Test Redis connection
	if err := utils.TestRedisConnection(); err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}
	log.Println(" Redis connection successful")

	connectors.BinanceConnector()
	connectors.UniswapConnector()

	ob, err := utils.GetFromOrderBook(context.Background(), "orderbook:uniswap-v3-exact:WBTC/USDT")
	if err != nil {
		log.Fatalf("Failed to get orderbook from Redis: %v", err)
	}
	log.Printf(" Got orderbook from Redis: %v", ob)

}
