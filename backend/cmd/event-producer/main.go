package main

import (
	"dynamic-pricing-engine/backend/internal/catalog"
	"dynamic-pricing-engine/backend/internal/config"
	"encoding/json"
	"log"
	"math/rand"
	"time"

	"github.com/IBM/sarama"
)

type ProductEvent struct {
	EventType string  `json:"event_type"` // "view", "cart", "purchase"
	ProductID string  `json:"product_id"`
	UserID    string  `json:"user_id"`
	Timestamp int64   `json:"timestamp"`
	Price     float64 `json:"price"` // catalog/base price for the product
}

var eventTypes = []string{"view", "cart", "purchase"}
var userIDs = []string{"U0", "U1", "U2", "U3", "U4"}
var productIDs = catalog.ProductIDs()

func generateRandomEvent() ProductEvent {
	productID := productIDs[rand.Intn(len(productIDs))]
	return ProductEvent{
		EventType: eventTypes[rand.Intn(len(eventTypes))],
		ProductID: productID,
		UserID:    userIDs[rand.Intn(len(userIDs))],
		Timestamp: time.Now().UnixMilli(),
		Price:     mustBasePrice(productID),
	}
}

func mustBasePrice(productID string) float64 {
	price, ok := catalog.BasePrice(productID)
	if !ok {
		log.Fatalf("missing catalog base price for product %s", productID)
	}
	return price
}

func main() {
	rand.Seed(time.Now().UnixNano())

	cfg, err := config.LoadEventProducerConfig()
	if err != nil {
		log.Fatalf("Invalid configuration: %v", err)
	}

	producer, err := sarama.NewSyncProducer(cfg.KafkaBrokers, nil)
	if err != nil {
		log.Fatalf("Failed to start Kafka producer: %v", err)
	}
	defer producer.Close()

	for {
		event := generateRandomEvent()
		value, err := json.Marshal(event)
		if err != nil {
			log.Printf("Failed to encode event: %v", err)
			continue
		}

		msg := &sarama.ProducerMessage{
			Topic: cfg.ProductEventsTopic,
			Value: sarama.ByteEncoder(value),
		}
		_, _, err = producer.SendMessage(msg)
		if err != nil {
			log.Printf("Failed to send message: %v", err)
		} else {
			log.Printf("Event sent to %s: %s", cfg.ProductEventsTopic, value)
		}
		time.Sleep(1 * time.Second)
	}
}
