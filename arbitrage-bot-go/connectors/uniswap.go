package connectors

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"os"
	"time"

	"github.com/Jkrish1011/SwapSleuth/arbitrage-bot-go/utils"
	"github.com/joho/godotenv"
)

// --- Data types for Uniswap subgraph response (trimmed) ---
type UniswapResponse struct {
	Data struct {
		Pools []struct {
			ID          string                            `json:"id"`
			Token0      struct{ Symbol, Decimals string } `json:"token0"`
			Token1      struct{ Symbol, Decimals string } `json:"token1"`
			Token0Price string                            `json:"token0Price"`
			Token1Price string                            `json:"token1Price"`
			SqrtPrice   string                            `json:"sqrtPrice"` // big integer
			Liquidity   string                            `json:"liquidity"` // big integer
			FeeTier     string                            `json:"feeTier"`
		} `json:"pools"`
	} `json:"data"`
}

// ---------- Helper big-float utilities ----------

// pow10BigFloat returns 10^dec as *big.Float (prec = 256).
func pow10BigFloat(dec int) *big.Float {
	bi := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(dec)), nil)
	f := new(big.Float).SetPrec(256).SetInt(bi)
	return f
}

// parseBigIntStringToBigFloat parses a decimal integer string to *big.Float (prec 256)
func parseBigIntStringToBigFloat(s string) (*big.Float, error) {
	i := new(big.Int)
	_, ok := i.SetString(s, 10)
	if !ok {
		return nil, fmt.Errorf("invalid integer: %s", s)
	}
	f := new(big.Float).SetPrec(256).SetInt(i)
	return f, nil
}

// ---------- Uniswap v3 single-range swap math ----------

// convertSqrtPriceX96ToFloat converts sqrtPriceX96 (big int as string) to sqrtP as big.Float:
// sqrtP = sqrtPriceX96 / 2^96
func convertSqrtPriceX96ToFloat(sqrtPriceX96Str string) (*big.Float, error) {
	sp, err := parseBigIntStringToBigFloat(sqrtPriceX96Str)
	if err != nil {
		return nil, err
	}
	den := new(big.Float).SetPrec(256)
	den.SetInt(new(big.Int).Lsh(big.NewInt(1), 96)) // 2^96
	res := new(big.Float).SetPrec(256).Quo(sp, den)
	return res, nil
}

// simulateToken0ToToken1 simulates selling amountToken0 (human units) and returns token1Out (human units).
// token0Decimals and token1Decimals are decimals for tokens (e.g., WBTC=8, USDT=6).
// sqrtPriceX96Str and liquidityStr are strings from subgraph.
func simulateToken0ToToken1(amountToken0 float64, token0Decimals int, token1Decimals int, sqrtPriceX96Str string, liquidityStr string, feeBps int) (float64, error) {
	// Precision
	prec := uint(256)

	// Convert inputs to big.Float raw units
	ten0 := pow10BigFloat(token0Decimals) // 10^dec0
	ten1 := pow10BigFloat(token1Decimals)

	amount0Human := new(big.Float).SetPrec(prec).SetFloat64(amountToken0)
	amount0Raw := new(big.Float).SetPrec(prec).Mul(amount0Human, ten0) // raw units

	// parse liquidity and sqrtPrice
	L, err := parseBigIntStringToBigFloat(liquidityStr)
	if err != nil {
		return 0, err
	}
	L.SetPrec(prec)

	sqrtP, err := convertSqrtPriceX96ToFloat(sqrtPriceX96Str)
	if err != nil {
		return 0, err
	}
	sqrtP.SetPrec(prec)

	// apply fee on input amount: feeBps is e.g. 3000 for 0.3%
	fee := new(big.Float).SetPrec(prec).Quo(new(big.Float).SetFloat64(float64(feeBps)), big.NewFloat(1e6)) // fee fraction
	// fee fraction = feeBps / 1e6 because feeBps like 3000 => 0.003
	one := new(big.Float).SetPrec(prec).SetFloat64(1.0)
	feeMultiplier := new(big.Float).SetPrec(prec).Sub(one, fee) // (1 - fee)

	amount0AfterFee := new(big.Float).SetPrec(prec).Mul(amount0Raw, feeMultiplier)

	// invSqrtP = 1 / sqrtP
	invSqrtP := new(big.Float).SetPrec(prec).Quo(one, sqrtP)

	// amount0 = L * (1/sqrtP - 1/sqrtP')  => 1/sqrtP' = 1/sqrtP - amount0/L
	amount0OverL := new(big.Float).SetPrec(prec).Quo(amount0AfterFee, L)
	invSqrtPPrime := new(big.Float).SetPrec(prec).Sub(invSqrtP, amount0OverL)

	// check invSqrtPPrime > 0
	if invSqrtPPrime.Sign() <= 0 {
		return 0, fmt.Errorf("trade too large: would cross infinite price (invSqrtPPrime <= 0)")
	}

	// sqrtP' = 1 / invSqrtPPrime
	sqrtPPrime := new(big.Float).SetPrec(prec).Quo(one, invSqrtPPrime)

	// amount1OutRaw = L * (sqrtP' - sqrtP)
	deltaSqrt := new(big.Float).SetPrec(prec).Sub(sqrtPPrime, sqrtP)
	amount1OutRaw := new(big.Float).SetPrec(prec).Mul(L, deltaSqrt)

	// Convert raw amount1 back to human units: amount1Human = amount1OutRaw / 10^dec1
	amount1Human := new(big.Float).SetPrec(prec).Quo(amount1OutRaw, ten1)

	// convert to float64 for returning (be careful with precision/overflow)
	f, _ := amount1Human.Float64()

	return f, nil
}

// simulateToken1ToToken0 simulates selling amountToken1 (human units) and returns token0Out (human units).
func simulateToken1ToToken0(amountToken1 float64, token0Decimals int, token1Decimals int, sqrtPriceX96Str string, liquidityStr string, feeBps int) (float64, error) {
	prec := uint(256)
	ten0 := pow10BigFloat(token0Decimals)
	ten1 := pow10BigFloat(token1Decimals)

	amount1Human := new(big.Float).SetPrec(prec).SetFloat64(amountToken1)
	amount1Raw := new(big.Float).SetPrec(prec).Mul(amount1Human, ten1)

	// parse liquidity and sqrtPrice
	L, err := parseBigIntStringToBigFloat(liquidityStr)
	if err != nil {
		return 0, err
	}
	L.SetPrec(prec)

	sqrtP, err := convertSqrtPriceX96ToFloat(sqrtPriceX96Str)
	if err != nil {
		return 0, err
	}
	sqrtP.SetPrec(prec)

	// apply fee on input amount
	fee := new(big.Float).SetPrec(prec).Quo(new(big.Float).SetFloat64(float64(feeBps)), big.NewFloat(1e6))
	one := new(big.Float).SetPrec(prec).SetFloat64(1.0)
	feeMultiplier := new(big.Float).SetPrec(prec).Sub(one, fee)

	amount1AfterFee := new(big.Float).SetPrec(prec).Mul(amount1Raw, feeMultiplier)

	// sqrtP' = sqrtP + amount1 / L
	amount1OverL := new(big.Float).SetPrec(prec).Quo(amount1AfterFee, L)
	sqrtPPrime := new(big.Float).SetPrec(prec).Add(sqrtP, amount1OverL)

	// amount0OutRaw = L * (1/sqrtP - 1/sqrtP')
	invSqrtP := new(big.Float).SetPrec(prec).Quo(one, sqrtP)
	invSqrtPPrime := new(big.Float).SetPrec(prec).Quo(one, sqrtPPrime)
	diffInv := new(big.Float).SetPrec(prec).Sub(invSqrtP, invSqrtPPrime)

	amount0OutRaw := new(big.Float).SetPrec(prec).Mul(L, diffInv)

	// convert back to human units
	amount0Human := new(big.Float).SetPrec(prec).Quo(amount0OutRaw, ten0)
	f, _ := amount0Human.Float64()
	return f, nil
}

// ----------------- Main connector function (uses the exact math) -----------------

func UniswapConnector() {
	// Load env for API key
	err := godotenv.Load()
	if err != nil {
		log.Println("No .env loaded (continuing)")
	}
	apiKey := os.Getenv("SUBGRAPH_API_KEY")

	// GraphQL query (largest WBTC/USDT pools)
	query := `{
		pools(
			where: {
				or: [
					{ token0_: {symbol: "WBTC"}, token1_: {symbol: "USDT"} },
					{ token0_: {symbol: "USDT"}, token1_: {symbol: "WBTC"} }
				]
			}
			orderBy: totalValueLockedUSD
			orderDirection: desc
			first: 5
		) {
			id
			token0 { symbol decimals }
			token1 { symbol decimals }
			token0Price
			token1Price
			sqrtPrice
			liquidity
			feeTier
		}
	}`

	payload := map[string]string{"query": query}
	requestBody, _ := json.Marshal(payload)

	client := &http.Client{}
	req, _ := http.NewRequest(http.MethodPost,
		"https://gateway.thegraph.com/api/subgraphs/id/5zvR82QoaXYFyDEKLZ9t6v9adgnptxYpKpSbxtgVENFV",
		bytes.NewBuffer(requestBody))
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		log.Println("error fetching subgraph:", err)
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var uniResp UniswapResponse
	if err := json.Unmarshal(body, &uniResp); err != nil {
		log.Println("json parse error:", err)
		return
	}
	if len(uniResp.Data.Pools) == 0 {
		log.Println("No pools found")
		return
	}

	// pick pool with highest liquidity (first in response ordered by TVL)
	pool := uniResp.Data.Pools[0]
	fmt.Println("Using pool:", pool.ID, "feeTier:", pool.FeeTier)

	// parse decimals
	var dec0, dec1 int
	fmt.Sscan(pool.Token0.Decimals, &dec0)
	fmt.Sscan(pool.Token1.Decimals, &dec1)

	// parse sqrtPrice and liquidity as strings (the functions expect strings)
	sqrtPriceStr := pool.SqrtPrice
	liquidityStr := pool.Liquidity

	// Fee tier (string) convert to bps*100? We'll treat feeTier like '3000' -> 0.003 => feeBps = 3000
	var feeBps int
	fmt.Sscan(pool.FeeTier, &feeBps)

	// choose synthetic sizes (human units). For token0 = WBTC (dec 8) sizes in BTC
	btcSizes := []float64{0.001, 0.005, 0.01} // small sizes
	usdtSizes := []float64{50, 200, 1000}     // USDT sizes

	bids := [][]float64{}
	for _, s := range btcSizes {
		out, err := simulateToken0ToToken1(s, dec0, dec1, sqrtPriceStr, liquidityStr, feeBps)
		if err != nil {
			log.Printf("simulateToken0ToToken1 error: %v", err)
			continue
		}
		// price = USDT_out / BTC_in
		price := out / s
		bids = append(bids, []float64{price, s})
	}

	asks := [][]float64{}
	for _, usdt := range usdtSizes {
		// simulate token1 -> token0
		outBTC, err := simulateToken1ToToken0(usdt, dec0, dec1, sqrtPriceStr, liquidityStr, feeBps)
		if err != nil {
			log.Printf("simulateToken1ToToken0 error: %v", err)
			continue
		}
		// price = USDT_spent / BTC_out
		if outBTC <= 0 {
			continue
		}
		price := usdt / outBTC
		asks = append(asks, []float64{price, outBTC})
	}

	ob := utils.NormalizationSchema{
		Exchange:  "uniswap-v3-exact",
		Pair:      pool.Token0.Symbol + "/" + pool.Token1.Symbol,
		Bids:      bids,
		Asks:      asks,
		Timestamp: time.Now().Unix(),
	}

	j, _ := json.MarshalIndent(ob, "", "  ")
	fmt.Println(string(j))

	utils.InitRedis()

	// Push to Redis
	err = utils.PushOrderbook(context.Background(), ob)
	if err != nil {
		fmt.Printf("error pushing orderbook to Redis: %v\n", err)
		return
	}

}
