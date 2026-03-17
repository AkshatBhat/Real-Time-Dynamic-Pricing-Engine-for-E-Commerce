package main

import (
	"fmt"
	"sort"
)

const historySourceBase = "base"

type baselineDependencies struct {
	productIDs        []string
	basePrice         func(productID string) (float64, bool)
	hasDemandKey      func(productID string) (bool, error)
	setDemandZero     func(productID string) error
	hasPriceKey       func(productID string) (bool, error)
	setPrice          func(productID string, price float64) error
	ensureBaseHistory func(productID string, price float64) error
}

func seedCatalogBaselines(deps baselineDependencies) error {
	productIDs := append([]string(nil), deps.productIDs...)
	sort.Strings(productIDs)

	for _, productID := range productIDs {
		price, ok := deps.basePrice(productID)
		if !ok {
			return fmt.Errorf("catalog base price missing for %s", productID)
		}

		demandExists, err := deps.hasDemandKey(productID)
		if err != nil {
			return fmt.Errorf("check demand key for %s: %w", productID, err)
		}
		if !demandExists {
			if err := deps.setDemandZero(productID); err != nil {
				return fmt.Errorf("set baseline demand for %s: %w", productID, err)
			}
		}

		priceExists, err := deps.hasPriceKey(productID)
		if err != nil {
			return fmt.Errorf("check price key for %s: %w", productID, err)
		}
		if !priceExists {
			if err := deps.setPrice(productID, price); err != nil {
				return fmt.Errorf("set baseline price for %s: %w", productID, err)
			}
		}

		if err := deps.ensureBaseHistory(productID, price); err != nil {
			return fmt.Errorf("ensure baseline history for %s: %w", productID, err)
		}
	}

	return nil
}
