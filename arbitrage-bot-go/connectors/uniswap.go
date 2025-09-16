package connectors

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/Jkrish1011/SwapSleuth/arbitrage-bot-go/utils"
	"github.com/joho/godotenv"
)

// Data structures for parsing Uniswap pool response
type UniswapResponse struct {
	Data struct {
		Pools []struct {
			ID     string `json:"id"`
			Token0 struct {
				Symbol   string `json:"symbol"`
				Decimals string `json:"decimals"`
			} `json:"token0"`
			Token1 struct {
				Symbol   string `json:"symbol"`
				Decimals string `json:"decimals"`
			} `json:"token1"`
			Token0Price string `json:"token0Price"`
			Token1Price string `json:"token1Price"`
			Liquidity   string `json:"liquidity"`
			SqrtPrice   string `json:"sqrtPrice"`
		} `json:"pools"`
	} `json:"data"`
}

/*
Slippage estimate:

price_out = midPrice * (1 ± k * size / liquidity)

*/
// simulateSwap approximates AMM slippage using pool liquidity
func simulateSwap(midPrice float64, size float64, liquidity float64, side string) float64 {
	// side = "buy" means spend USDT to get BTC (ask)
	// side = "sell" means sell BTC for USDT (bid)

	// Slippage factor: size / liquidity
	slippage := (size / liquidity) * 1000 // scale to exaggerate
	if side == "buy" {
		return midPrice * (1 + slippage)
	} else {
		return midPrice * (1 - slippage)
	}
}

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
					{ token0_: {symbol: "WBTC"}, token1_: {symbol: "USDT"} }
					{ token0_: {symbol: "USDT"}, token1_: {symbol: "WBTC"} }
				]
			}
			orderBy: totalValueLockedUSD
			orderDirection: desc
			first: 1
		) {
			id
			token0 { symbol decimals }
			token1 { symbol decimals }
			token0Price
			token1Price
			sqrtPrice
			liquidity
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
	var uniResp UniswapResponse
	err = json.Unmarshal(subgraphResponseBody, &uniResp)
	if err != nil {
		log.Println("json parse error:", err)
		return
	}

	if len(uniResp.Data.Pools) == 0 {
		log.Println("No pools found")
		return
	}

	pool := uniResp.Data.Pools[0]
	fmt.Println("Using pool:", pool.ID)

	// Parse numbers
	var midPrice float64
	json.Unmarshal([]byte(pool.Token1Price), &midPrice)
	var liquidity float64
	json.Unmarshal([]byte(pool.Liquidity), &liquidity)

	// Define trade sizes
	btcSizes := []float64{0.01, 0.05, 0.1}
	usdtSizes := []float64{100, 500, 1000}

	bids := [][]float64{}
	for _, size := range btcSizes {
		price := simulateSwap(midPrice, size, liquidity, "sell")
		bids = append(bids, []float64{price, size})
	}

	asks := [][]float64{}
	for _, size := range usdtSizes {
		// Approx BTC size
		btcOut := float64(size) / midPrice
		price := simulateSwap(midPrice, btcOut, liquidity, "buy")
		asks = append(asks, []float64{price, btcOut})
	}

	ob := utils.NormalizationSchema{
		Exchange:  "uniswap-v3",
		Pair:      "WBTCUSDT",
		Bids:      bids,
		Asks:      asks,
		Timestamp: time.Now().Unix(),
	}

	j, _ := json.MarshalIndent(ob, "", "  ")
	fmt.Println(string(j))
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
