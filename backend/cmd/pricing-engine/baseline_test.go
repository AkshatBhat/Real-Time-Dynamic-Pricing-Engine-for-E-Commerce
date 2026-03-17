package main

import (
	"errors"
	"slices"
	"testing"
)

func TestSeedCatalogBaselinesSeedsMissingValues(t *testing.T) {
	var demandSeeded []string
	var priceSeeded []string
	var historySeeded []string

	deps := baselineDependencies{
		productIDs: []string{"PB", "PA"},
		basePrice: func(productID string) (float64, bool) {
			switch productID {
			case "PA":
				return 100, true
			case "PB":
				return 200, true
			default:
				return 0, false
			}
		},
		hasDemandKey: func(productID string) (bool, error) {
			return productID == "PB", nil
		},
		setDemandZero: func(productID string) error {
			demandSeeded = append(demandSeeded, productID)
			return nil
		},
		hasPriceKey: func(productID string) (bool, error) {
			return false, nil
		},
		setPrice: func(productID string, price float64) error {
			priceSeeded = append(priceSeeded, productID)
			return nil
		},
		ensureBaseHistory: func(productID string, price float64) error {
			historySeeded = append(historySeeded, productID)
			return nil
		},
	}

	if err := seedCatalogBaselines(deps); err != nil {
		t.Fatalf("seedCatalogBaselines returned error: %v", err)
	}

	if !slices.Equal(demandSeeded, []string{"PA"}) {
		t.Fatalf("unexpected demand seed list: %#v", demandSeeded)
	}
	if !slices.Equal(priceSeeded, []string{"PA", "PB"}) {
		t.Fatalf("unexpected price seed list: %#v", priceSeeded)
	}
	if !slices.Equal(historySeeded, []string{"PA", "PB"}) {
		t.Fatalf("unexpected history seed list: %#v", historySeeded)
	}
}

func TestSeedCatalogBaselinesMissingBasePrice(t *testing.T) {
	deps := baselineDependencies{
		productIDs: []string{"PA"},
		basePrice: func(productID string) (float64, bool) {
			return 0, false
		},
	}

	if err := seedCatalogBaselines(deps); err == nil {
		t.Fatalf("expected error, got nil")
	}
}

func TestSeedCatalogBaselinesReturnsDependencyError(t *testing.T) {
	expectedErr := errors.New("boom")
	deps := baselineDependencies{
		productIDs: []string{"PA"},
		basePrice: func(productID string) (float64, bool) {
			return 100, true
		},
		hasDemandKey: func(productID string) (bool, error) {
			return false, expectedErr
		},
	}

	err := seedCatalogBaselines(deps)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected wrapped error %v, got %v", expectedErr, err)
	}
}
