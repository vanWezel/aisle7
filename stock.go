package main

// Stock modes filter by availability, using the same words as the rental cards.
const (
	StockAny       = iota // no filter
	StockMine             // IN STOCK: streaming on one of your services
	StockStreaming        // streaming on any service
	StockRental           // RENTAL ONLY: rent/buy but not streaming
	StockAvail            // streaming or rent/buy: anything you can watch
	StockOut              // OUT OF STOCK: no offers at all
)

var stockLabels = []string{"Any", "In stock (mine)", "Streaming", "Rental only", "Streaming + rental", "Out of stock"}

var streamOfferTypes = []string{"FLATRATE", "FLATRATE_AND_BUY", "FREE", "ADS"}

// applyStock adds the server-side part of a stock mode to a JustWatch filter.
// Rental-only and out-of-stock also need matchStock on the results.
func applyStock(filter map[string]any, mode int, shorts []string) {
	switch mode {
	case StockMine:
		filter["packages"] = shorts
		filter["monetizationTypes"] = streamOfferTypes
	case StockStreaming:
		filter["monetizationTypes"] = streamOfferTypes
	case StockRental:
		filter["monetizationTypes"] = []string{"RENT", "BUY"}
	case StockAvail:
		filter["monetizationTypes"] = append(append([]string{}, streamOfferTypes...), "RENT", "BUY")
	}
}

// matchStock is the client-side part: it removes what the server can't.
func matchStock(m Movie, mode int) bool {
	switch mode {
	case StockMine, StockStreaming:
		return len(m.Streaming) > 0 // JustWatch also counts TV-box-only offers
	case StockAvail:
		return len(m.Streaming) > 0 || m.RentBuy
	case StockRental:
		return len(m.Streaming) == 0 && m.RentBuy
	case StockOut:
		return len(m.Streaming) == 0 && !m.RentBuy
	}
	return true
}

// needsLocalStock reports whether results are filtered after fetching, which
// only sees the most popular matches rather than every one.
func needsLocalStock(mode int) bool { return mode == StockRental || mode == StockOut }

// stockPool is how many top matches to fetch for a mode.
func stockPool(mode int) int {
	if needsLocalStock(mode) {
		return 300
	}
	return 100
}
