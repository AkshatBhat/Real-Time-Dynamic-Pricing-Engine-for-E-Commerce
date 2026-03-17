//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"dynamic-pricing-engine/backend/internal/config"

	"github.com/IBM/sarama"
	"github.com/gorilla/websocket"
)

type productEvent struct {
	EventType string  `json:"event_type"`
	ProductID string  `json:"product_id"`
	UserID    string  `json:"user_id"`
	Timestamp int64   `json:"timestamp"`
	Price     float64 `json:"price"`
}

type wsPriceUpdateEvent struct {
	Type      string  `json:"type"`
	ProductID string  `json:"product_id"`
	NewPrice  float64 `json:"new_price"`
	Timestamp int64   `json:"timestamp"`
}

func TestWebSocketPriceUpdateSmoke(t *testing.T) {
	apiCfg, err := config.LoadAPIConfig()
	if err != nil {
		t.Fatalf("load API config: %v", err)
	}
	pricingCfg, err := config.LoadPricingEngineConfig()
	if err != nil {
		t.Fatalf("load pricing config: %v", err)
	}

	wsURL := fmt.Sprintf("ws://localhost:%d/ws/prices", apiCfg.Port)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("connect websocket %q: %v", wsURL, err)
	}
	defer conn.Close()

	producer, err := sarama.NewSyncProducer(pricingCfg.KafkaBrokers, nil)
	if err != nil {
		t.Fatalf("create kafka producer: %v", err)
	}
	defer producer.Close()

	productID := "PA"
	sentAt := time.Now().Unix() - 1

	rawEvent, err := json.Marshal(productEvent{
		EventType: "view",
		ProductID: productID,
		UserID:    "integration-smoke",
		Timestamp: time.Now().Unix(),
		Price:     200,
	})
	if err != nil {
		t.Fatalf("marshal product event: %v", err)
	}

	msg := &sarama.ProducerMessage{
		Topic: pricingCfg.ProductEventsTopic,
		Value: sarama.ByteEncoder(rawEvent),
	}
	if _, _, err := producer.SendMessage(msg); err != nil {
		t.Fatalf("publish product event: %v", err)
	}

	deadline := time.Now().Add(20 * time.Second)
	_ = conn.SetReadDeadline(deadline)

	for time.Now().Before(deadline) {
		_, payload, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read websocket message: %v", err)
		}

		lines := strings.Split(strings.TrimSpace(string(payload)), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}

			var update wsPriceUpdateEvent
			if err := json.Unmarshal([]byte(line), &update); err != nil {
				continue
			}
			if update.Type != "price_update" {
				continue
			}
			if update.ProductID == productID && update.Timestamp >= sentAt {
				return
			}
		}
	}

	t.Fatalf("timed out waiting for websocket price_update event for %s", productID)
}
