package main

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

func TestValidateEvent(t *testing.T) {
	valid := ProductEvent{
		EventType: "view",
		ProductID: "PA",
		Price:     100,
	}
	if err := validateEvent(valid); err != nil {
		t.Fatalf("expected valid event, got error: %v", err)
	}

	invalidType := valid
	invalidType.EventType = "click"
	if err := validateEvent(invalidType); err == nil {
		t.Fatalf("expected invalid event type error")
	}

	invalidProduct := valid
	invalidProduct.ProductID = "  "
	if err := validateEvent(invalidProduct); err == nil {
		t.Fatalf("expected invalid product_id error")
	}

	invalidPrice := valid
	invalidPrice.Price = 0
	if err := validateEvent(invalidPrice); err == nil {
		t.Fatalf("expected invalid price error")
	}
}

func TestDemandWeight(t *testing.T) {
	testCases := []struct {
		name      string
		eventType string
		want      int64
		wantErr   bool
	}{
		{name: "view", eventType: "view", want: 1},
		{name: "cart", eventType: "cart", want: 3},
		{name: "purchase", eventType: "purchase", want: 5},
		{name: "invalid", eventType: "random", wantErr: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := demandWeight(tc.eventType)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for event type %q", tc.eventType)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("unexpected weight: got %d, want %d", got, tc.want)
			}
		})
	}
}

func TestComputePrice(t *testing.T) {
	got := computePrice(100, 9)
	want := 115.0 // 100 + sqrt(9)*5
	if math.Abs(got-want) > 0.00001 {
		t.Fatalf("unexpected computed price: got %.5f, want %.5f", got, want)
	}
}

func TestProcessIncomingEventPersistsBeforePublish(t *testing.T) {
	rawEvent, err := json.Marshal(ProductEvent{
		EventType: "purchase",
		ProductID: "PA",
		Price:     120,
	})
	if err != nil {
		t.Fatalf("failed to marshal test event: %v", err)
	}

	callOrder := make([]string, 0, 8)
	publishCalls := 0

	deps := processorDependencies{
		incrementDemand: func(productID string, delta int64) (int64, error) {
			callOrder = append(callOrder, "increment")
			if productID != "PA" || delta != 5 {
				t.Fatalf("unexpected increment input: product=%s delta=%d", productID, delta)
			}
			return 9, nil
		},
		persistPrice: func(productID string, price float64) error {
			callOrder = append(callOrder, "persist_price")
			return nil
		},
		persistHistory: func(productID string, price float64) error {
			callOrder = append(callOrder, "persist_history")
			return nil
		},
		publishUpdate: func(update PriceUpdate) error {
			publishCalls++
			callOrder = append(callOrder, "publish")
			return errors.New("temporary kafka failure")
		},
		nowUnix: func() int64 { return 1700000000 },
		sleep: func(duration time.Duration) {
			callOrder = append(callOrder, "sleep")
		},
	}

	err = processIncomingEvent(rawEvent, deps)
	if err == nil {
		t.Fatalf("expected publish failure after retries")
	}
	if !strings.Contains(err.Error(), "publish price update after retries") {
		t.Fatalf("unexpected error: %v", err)
	}

	if publishCalls != 4 { // initial publish + 3 retries
		t.Fatalf("unexpected publish call count: got %d, want 4", publishCalls)
	}

	firstPublishIndex := -1
	for i, step := range callOrder {
		if step == "publish" {
			firstPublishIndex = i
			break
		}
	}
	if firstPublishIndex == -1 {
		t.Fatalf("publish was never called")
	}

	for _, requiredStep := range []string{"increment", "persist_price", "persist_history"} {
		found := false
		for idx := 0; idx < firstPublishIndex; idx++ {
			if callOrder[idx] == requiredStep {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected %s to run before first publish; call order: %#v", requiredStep, callOrder)
		}
	}
}

func TestProcessIncomingEventRejectsInvalidPayload(t *testing.T) {
	deps := processorDependencies{
		incrementDemand: func(productID string, delta int64) (int64, error) {
			t.Fatalf("incrementDemand should not be called for invalid payload")
			return 0, nil
		},
		persistPrice: func(productID string, price float64) error {
			t.Fatalf("persistPrice should not be called for invalid payload")
			return nil
		},
		persistHistory: func(productID string, price float64) error {
			t.Fatalf("persistHistory should not be called for invalid payload")
			return nil
		},
		publishUpdate: func(update PriceUpdate) error {
			t.Fatalf("publishUpdate should not be called for invalid payload")
			return nil
		},
		nowUnix: func() int64 { return 0 },
		sleep:   func(duration time.Duration) {},
	}

	rawEvent, err := json.Marshal(ProductEvent{
		EventType: "unknown",
		ProductID: "PA",
		Price:     100,
	})
	if err != nil {
		t.Fatalf("failed to marshal test event: %v", err)
	}

	if err := processIncomingEvent(rawEvent, deps); err == nil {
		t.Fatalf("expected invalid event to be rejected")
	}
}
