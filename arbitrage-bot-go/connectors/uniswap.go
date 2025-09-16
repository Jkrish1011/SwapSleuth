package connectors

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/joho/godotenv"
)

// Query for WBTC/USDT pool data with current price and liquidity
func UniswapConnector() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}
	apiKey := os.Getenv("SUBGRAPH_API_KEY")

	// Query to find WBTC/USDT pools and get current price data
	query := `{
		pools(
			where: {
				or: [
					{
						token0_: {symbol: "WBTC"}
						token1_: {symbol: "USDT"}
					}
					{
						token0_: {symbol: "USDT"}
						token1_: {symbol: "WBTC"}
					}
				]
			}
			orderBy: totalValueLockedUSD
			orderDirection: desc
			first: 5
		) {
			id
			token0 {
				id
				symbol
				name
				decimals
			}
			token1 {
				id
				symbol
				name
				decimals
			}
			token0Price
			token1Price
			sqrtPrice
			tick
			liquidity
			totalValueLockedToken0
			totalValueLockedToken1
			totalValueLockedUSD
			volumeUSD
			feeTier
		}
	}`

	payload := map[string]string{
		"query": query,
	}

	requestBody, err := json.Marshal(payload)
	if err != nil {
		log.Println("error marshaling JSON:", err)
		return
	}

	client := &http.Client{}
	subgraphRequest, err := http.NewRequest(http.MethodPost, "https://gateway.thegraph.com/api/subgraphs/id/5zvR82QoaXYFyDEKLZ9t6v9adgnptxYpKpSbxtgVENFV", bytes.NewBuffer(requestBody))
	if err != nil {
		log.Println("error:", err)
		return
	}

	subgraphRequest.Header.Set("Authorization", "Bearer "+apiKey)
	subgraphRequest.Header.Set("Accept", "application/json")
	subgraphRequest.Header.Set("Content-Type", "application/json")

	subgraphResponse, err := client.Do(subgraphRequest)
	if err != nil {
		log.Println("error:", err)
		return
	}
	defer subgraphResponse.Body.Close()

	subgraphResponseBody, err := io.ReadAll(subgraphResponse.Body)
	if err != nil {
		log.Println("error:", err)
		return
	}
	log.Println(string(subgraphResponseBody))
}

// Query for recent swaps (transactions) in WBTC/USDT pools - this is the closest to "market activity"
func GetRecentWBTCUSDTSwaps() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}
	apiKey := os.Getenv("SUBGRAPH_API_KEY")

	// Query recent swaps in WBTC/USDT pools
	query := `{
		swaps(
			where: {
				pool_: {
					or: [
						{
							token0_: {symbol: "WBTC"}
							token1_: {symbol: "USDT"}
						}
						{
							token0_: {symbol: "USDT"}
							token1_: {symbol: "WBTC"}
						}
					]
				}
			}
			orderBy: timestamp
			orderDirection: desc
			first: 10
		) {
			id
			timestamp
			pool {
				id
				token0 {
					symbol
				}
				token1 {
					symbol
				}
			}
			sender
			recipient
			amount0
			amount1
			amountUSD
			sqrtPriceX96
			tick
		}
	}`

	payload := map[string]string{
		"query": query,
	}

	requestBody, err := json.Marshal(payload)
	if err != nil {
		log.Println("error marshaling JSON:", err)
		return
	}

	client := &http.Client{}
	subgraphRequest, err := http.NewRequest(http.MethodPost, "https://gateway.thegraph.com/api/subgraphs/id/5zvR82QoaXYFyDEKLZ9t6v9adgnptxYpKpSbxtgVENFV", bytes.NewBuffer(requestBody))
	if err != nil {
		log.Println("error:", err)
		return
	}

	subgraphRequest.Header.Set("Authorization", "Bearer "+apiKey)
	subgraphRequest.Header.Set("Accept", "application/json")
	subgraphRequest.Header.Set("Content-Type", "application/json")

	subgraphResponse, err := client.Do(subgraphRequest)
	if err != nil {
		log.Println("error:", err)
		return
	}
	defer subgraphResponse.Body.Close()

	subgraphResponseBody, err := io.ReadAll(subgraphResponse.Body)
	if err != nil {
		log.Println("error:", err)
		return
	}
	log.Println(string(subgraphResponseBody))
}

// Query for liquidity positions (ticks) around current price - closest to order book depth
func GetWBTCUSDTLiquidityDepth() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}
	apiKey := os.Getenv("SUBGRAPH_API_KEY")

	// First, get the current tick of the main WBTC/USDT pool
	// Then query ticks around that price point
	query := `{
		ticks(
			where: {
				pool_: {
					or: [
						{
							token0_: {symbol: "WBTC"}
							token1_: {symbol: "USDT"}
						}
						{
							token0_: {symbol: "USDT"}
							token1_: {symbol: "WBTC"}
						}
					]
				}
				liquidityGross_gt: "0"
			}
			orderBy: tickIdx
			first: 20
		) {
			id
			tickIdx
			liquidityGross
			liquidityNet
			price0
			price1
			pool {
				id
				token0 {
					symbol
				}
				token1 {
					symbol
				}
			}
		}
	}`

	payload := map[string]string{
		"query": query,
	}

	requestBody, err := json.Marshal(payload)
	if err != nil {
		log.Println("error marshaling JSON:", err)
		return
	}

	client := &http.Client{}
	subgraphRequest, err := http.NewRequest(http.MethodPost, "https://gateway.thegraph.com/api/subgraphs/id/5zvR82QoaXYFyDEKLZ9t6v9adgnptxYpKpSbxtgVENFV", bytes.NewBuffer(requestBody))
	if err != nil {
		log.Println("error:", err)
		return
	}

	subgraphRequest.Header.Set("Authorization", "Bearer "+apiKey)
	subgraphRequest.Header.Set("Accept", "application/json")
	subgraphRequest.Header.Set("Content-Type", "application/json")

	subgraphResponse, err := client.Do(subgraphRequest)
	if err != nil {
		log.Println("error:", err)
		return
	}
	defer subgraphResponse.Body.Close()

	subgraphResponseBody, err := io.ReadAll(subgraphResponse.Body)
	if err != nil {
		log.Println("error:", err)
		return
	}
	log.Println(string(subgraphResponseBody))
}
