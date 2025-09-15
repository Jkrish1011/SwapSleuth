package connectors

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/Jkrish1011/SwapSleuth/arbitrage-bot-go/utils"
)

// OrderBook represents the Binance order book structure
type OrderBook struct {
	LastUpdateID int64      `json:"lastUpdateId"`
	Bids         [][]string `json:"bids"` // [price, qty][]
	Asks         [][]string `json:"asks"` // [price, qty][]
}

func BinanceConnector() {
	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	// resp, err := client.Get("https://testnet.binance.vision/api/v3/ticker/price?symbol=BTCUSDT")
	resp, err := client.Get("https://testnet.binance.vision/api/v3/depth?symbol=BTCUSDT&limit=100")
	// resp, err := client.Get("https://testnet.binance.vision/api/w3/BTCUSDT@depth@100ms")

	if err != nil {
		fmt.Println("error:", err)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("error reading response body: %v\n", err)
		return
	}

	var orderBook OrderBook
	err = json.Unmarshal(body, &orderBook)
	if err != nil {
		fmt.Printf("error unmarshaling JSON: %v\n", err)
		return
	}

	// Now you can work with the structured data
	fmt.Printf("Status: %s\n", resp.Status)
	fmt.Printf("Last Update ID: %d\n", orderBook.LastUpdateID)

	var normalizedValue utils.NormalizationSchema

	normalizedValue.Exchange = "binance"
	normalizedValue.Pair = "BTCUSDT"
	normalizedValue.Timestamp = orderBook.LastUpdateID

	// Example: Print first 5 bids and asks
	fmt.Println("\nTop 5 Bids:")
	for i := 0; i < 5 && i < len(orderBook.Bids); i++ {
		fmt.Printf("Price: %s, Quantity: %s\n", orderBook.Bids[i][0], orderBook.Bids[i][1])
		p, _ := strconv.ParseFloat(orderBook.Bids[i][0], 64)
		quan, _ := strconv.ParseFloat(orderBook.Bids[i][1], 64)
		normalizedValue.Bids = append(normalizedValue.Bids, []float64{p, quan})
	}

	fmt.Println("\nTop 5 Asks:")
	for i := 0; i < 5 && i < len(orderBook.Asks); i++ {
		fmt.Printf("Price: %s, Quantity: %s\n", orderBook.Asks[i][0], orderBook.Asks[i][1])
		p, _ := strconv.ParseFloat(orderBook.Asks[i][0], 64)
		quan, _ := strconv.ParseFloat(orderBook.Asks[i][1], 64)
		normalizedValue.Asks = append(normalizedValue.Asks, []float64{p, quan})
	}

	// Pretty JSON
	b, err := json.MarshalIndent(normalizedValue, "", "  ")
	if err != nil {
		fmt.Printf("json marshal: %v", err)
	}
	fmt.Println(string(b))
}
