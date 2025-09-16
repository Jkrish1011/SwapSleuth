package main

import "github.com/Jkrish1011/SwapSleuth/arbitrage-bot-go/connectors"

func main() {
	connectors.BinanceConnector()
	connectors.UniswapConnector()
}
