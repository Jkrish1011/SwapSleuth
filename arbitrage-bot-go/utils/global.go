package utils

type NormalizationSchema struct {
	Exchange  string      `json:"exchange"`
	Pair      string      `json:"pair"`
	Bids      [][]float64 `json:"bids"`
	Asks      [][]float64 `json:"asks"`
	Timestamp int64       `json:"timestamp"`
}
