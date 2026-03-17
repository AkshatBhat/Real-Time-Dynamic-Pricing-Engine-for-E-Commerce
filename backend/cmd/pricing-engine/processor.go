package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"strings"
	"time"
)

var (
	errInvalidEventType = errors.New("invalid event_type")
	errInvalidProductID = errors.New("invalid product_id")
	errInvalidPrice     = errors.New("invalid price")
)

var eventWeights = map[string]int64{
	"view":     1,
	"cart":     3,
	"purchase": 5,
}

type processorDependencies struct {
	incrementDemand func(productID string, delta int64) (int64, error)
	persistPrice    func(productID string, price float64) error
	persistHistory  func(productID string, price float64) error
	publishUpdate   func(update PriceUpdate) error
	nowUnix         func() int64
	sleep           func(duration time.Duration)
}

func processIncomingEvent(rawMessage []byte, deps processorDependencies) error {
	event, err := decodeEvent(rawMessage)
	if err != nil {
		return err
	}

	if err := validateEvent(event); err != nil {
		return err
	}

	weight, err := demandWeight(event.EventType)
	if err != nil {
		return err
	}

	demand, err := deps.incrementDemand(event.ProductID, weight)
	if err != nil {
		return fmt.Errorf("increment demand: %w", err)
	}

	newPrice := computePrice(event.Price, demand)
	logPriceComputation(event.ProductID, newPrice, demand)

	if err := deps.persistPrice(event.ProductID, newPrice); err != nil {
		return fmt.Errorf("persist redis price: %w", err)
	}

	if err := deps.persistHistory(event.ProductID, newPrice); err != nil {
		return fmt.Errorf("persist oracle history: %w", err)
	}

	update := PriceUpdate{
		ProductID: event.ProductID,
		NewPrice:  newPrice,
		Timestamp: deps.nowUnix(),
	}

	if err := publishUpdateWithRetry(update, deps.publishUpdate, deps.sleep); err != nil {
		return fmt.Errorf("publish price update after retries: %w", err)
	}

	return nil
}

func decodeEvent(rawMessage []byte) (ProductEvent, error) {
	var event ProductEvent
	if err := json.Unmarshal(rawMessage, &event); err != nil {
		return ProductEvent{}, fmt.Errorf("decode event payload: %w", err)
	}
	return event, nil
}

func validateEvent(event ProductEvent) error {
	if _, ok := eventWeights[event.EventType]; !ok {
		return fmt.Errorf("%w: %q", errInvalidEventType, event.EventType)
	}
	if strings.TrimSpace(event.ProductID) == "" {
		return errInvalidProductID
	}
	if event.Price <= 0 {
		return fmt.Errorf("%w: %.2f", errInvalidPrice, event.Price)
	}
	return nil
}

func demandWeight(eventType string) (int64, error) {
	weight, ok := eventWeights[eventType]
	if !ok {
		return 0, fmt.Errorf("%w: %q", errInvalidEventType, eventType)
	}
	return weight, nil
}

func computePrice(eventPrice float64, demand int64) float64 {
	return eventPrice + math.Sqrt(float64(demand))*5
}

func publishUpdateWithRetry(update PriceUpdate, publish func(update PriceUpdate) error, sleep func(duration time.Duration)) error {
	retryDelays := []time.Duration{
		200 * time.Millisecond,
		500 * time.Millisecond,
		1 * time.Second,
	}

	if err := publish(update); err == nil {
		return nil
	}

	var lastErr error
	for _, delay := range retryDelays {
		sleep(delay)
		lastErr = publish(update)
		if lastErr == nil {
			return nil
		}
	}

	return lastErr
}

func demandKey(productID string) string {
	return fmt.Sprintf("product:demand:%s", productID)
}

func priceKey(productID string) string {
	return fmt.Sprintf("product:price:%s", productID)
}

func logPriceComputation(productID string, newPrice float64, demand int64) {
	log.Printf("💸 Product %s new price: %.2f (demand: %d)", productID, newPrice, demand)
}

func timeNowUnix() int64 {
	return time.Now().Unix()
}

func sleepWithDuration(duration time.Duration) {
	time.Sleep(duration)
}
