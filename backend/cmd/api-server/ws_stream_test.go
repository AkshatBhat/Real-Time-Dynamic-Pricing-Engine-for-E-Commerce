package main

import (
	"encoding/json"
	"testing"
	"time"
)

func TestParsePriceUpdateValid(t *testing.T) {
	raw := []byte(`{"product_id":"PA","new_price":123.45,"timestamp":1710000000}`)

	update, err := parsePriceUpdate(raw)
	if err != nil {
		t.Fatalf("expected valid price update, got error: %v", err)
	}
	if update.Type != wsEventTypePriceUpdate {
		t.Fatalf("unexpected type: %q", update.Type)
	}
	if update.ProductID != "PA" {
		t.Fatalf("unexpected product_id: %q", update.ProductID)
	}
	if update.NewPrice != 123.45 {
		t.Fatalf("unexpected new_price: %f", update.NewPrice)
	}
	if update.Timestamp != 1710000000 {
		t.Fatalf("unexpected timestamp: %d", update.Timestamp)
	}
}

func TestParsePriceUpdateInvalid(t *testing.T) {
	tests := []struct {
		name string
		raw  []byte
	}{
		{
			name: "malformed json",
			raw:  []byte(`{"product_id":"PA"`),
		},
		{
			name: "missing product",
			raw:  []byte(`{"product_id":" ","new_price":123.45,"timestamp":1710000000}`),
		},
		{
			name: "invalid price",
			raw:  []byte(`{"product_id":"PA","new_price":0,"timestamp":1710000000}`),
		},
		{
			name: "invalid timestamp",
			raw:  []byte(`{"product_id":"PA","new_price":123.45,"timestamp":0}`),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parsePriceUpdate(tc.raw); err == nil {
				t.Fatalf("expected invalid payload to fail")
			}
		})
	}
}

func TestPriceUpdateHubBroadcastsToRegisteredClient(t *testing.T) {
	hub := newPriceUpdateHub()
	go hub.run()
	defer hub.shutdown()

	client := &wsClient{
		hub:  hub,
		send: make(chan []byte, 1),
	}

	hub.register <- client
	time.Sleep(20 * time.Millisecond)

	hub.broadcastUpdate(wsPriceUpdateEvent{
		Type:      wsEventTypePriceUpdate,
		ProductID: "PA",
		NewPrice:  150.25,
		Timestamp: 1710000000,
	})

	select {
	case payload := <-client.send:
		var got wsPriceUpdateEvent
		if err := json.Unmarshal(payload, &got); err != nil {
			t.Fatalf("failed to decode hub payload: %v", err)
		}
		if got.ProductID != "PA" || got.NewPrice != 150.25 || got.Type != wsEventTypePriceUpdate {
			t.Fatalf("unexpected payload: %#v", got)
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("timed out waiting for hub broadcast")
	}
}

func TestPriceUpdateHubDropsSlowClient(t *testing.T) {
	hub := newPriceUpdateHub()
	go hub.run()
	defer hub.shutdown()

	slowClient := &wsClient{
		hub:  hub,
		send: make(chan []byte, 1),
	}
	slowClient.send <- []byte("occupied")

	hub.register <- slowClient
	time.Sleep(20 * time.Millisecond)

	hub.broadcastUpdate(wsPriceUpdateEvent{
		Type:      wsEventTypePriceUpdate,
		ProductID: "PA",
		NewPrice:  150.25,
		Timestamp: 1710000000,
	})

	select {
	case <-slowClient.send:
	case <-time.After(1 * time.Second):
		t.Fatalf("timed out waiting to drain slow client buffer")
	}

	select {
	case _, ok := <-slowClient.send:
		if ok {
			t.Fatalf("expected slow client send channel to be closed")
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("timed out waiting for slow client cleanup")
	}
}
