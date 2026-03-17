package catalog

import "sort"

var basePrices = map[string]float64{
	"PA": 299,
	"PB": 349,
	"PC": 129,
	"PD": 499,
	"PE": 219,
	"PF": 179,
	"PG": 649,
	"PH": 269,
	"PI": 399,
	"PJ": 159,
}

// ProductIDs returns the known catalog product IDs.
func ProductIDs() []string {
	ids := make([]string, 0, len(basePrices))
	for productID := range basePrices {
		ids = append(ids, productID)
	}
	sort.Strings(ids)
	return ids
}

// BasePrice returns the catalog base price for a product.
func BasePrice(productID string) (float64, bool) {
	price, ok := basePrices[productID]
	return price, ok
}
