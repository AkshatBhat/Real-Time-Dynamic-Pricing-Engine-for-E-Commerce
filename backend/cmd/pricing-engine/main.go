package main

import (
	"context"
	"dynamic-pricing-engine/backend/internal/catalog"
	"dynamic-pricing-engine/backend/internal/config"
	"dynamic-pricing-engine/backend/internal/db"
	"dynamic-pricing-engine/backend/internal/overridestate"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/IBM/sarama"
	"github.com/redis/go-redis/v9"
)

type ProductEvent struct {
	EventType string  `json:"event_type"`
	ProductID string  `json:"product_id"`
	UserID    string  `json:"user_id"`
	Timestamp int64   `json:"timestamp"`
	Price     float64 `json:"price"`
}

type PriceUpdate struct {
	ProductID string  `json:"product_id"`
	NewPrice  float64 `json:"new_price"`
	Timestamp int64   `json:"timestamp"`
}

var ctx = context.Background()

func main() {
	cfg, err := config.LoadPricingEngineConfig()
	if err != nil {
		log.Fatalf("Invalid configuration: %v", err)
	}

	// Redis client
	rdb := redis.NewClient(&redis.Options{
		Addr: cfg.RedisAddr,
	})

	// Oracle DB connection
	if err := db.InitOracle(cfg.OracleDSN, cfg.OracleLibDir, cfg.OracleTimezone); err != nil {
		log.Fatalf("Oracle init failed: %v", err)
	}

	if err := seedCatalogBaselines(baselineDependencies{
		productIDs: catalog.ProductIDs(),
		basePrice:  catalog.BasePrice,
		hasDemandKey: func(productID string) (bool, error) {
			count, err := rdb.Exists(ctx, demandKey(productID)).Result()
			if err != nil {
				return false, err
			}
			return count > 0, nil
		},
		setDemandZero: func(productID string) error {
			return rdb.Set(ctx, demandKey(productID), 0, 0).Err()
		},
		hasPriceKey: func(productID string) (bool, error) {
			count, err := rdb.Exists(ctx, priceKey(productID)).Result()
			if err != nil {
				return false, err
			}
			return count > 0, nil
		},
		setPrice: func(productID string, price float64) error {
			return rdb.Set(ctx, priceKey(productID), price, 0).Err()
		},
		ensureBaseHistory: db.EnsureBasePriceHistory,
	}); err != nil {
		log.Fatalf("Baseline initialization failed: %v", err)
	}

	// Kafka consumer
	kafkaConfig := sarama.NewConfig()
	kafkaConfig.Consumer.Return.Errors = true
	kafkaConfig.Version = sarama.V2_5_0_0

	consumer, err := sarama.NewConsumer(cfg.KafkaBrokers, kafkaConfig)
	if err != nil {
		log.Fatalf("Kafka consumer error: %v", err)
	}
	defer consumer.Close()

	partitionConsumer, err := consumer.ConsumePartition(cfg.ProductEventsTopic, 0, sarama.OffsetNewest)
	if err != nil {
		log.Fatalf("Partition consumer error: %v", err)
	}
	defer partitionConsumer.Close()

	// Producer for price updates
	producer, err := sarama.NewSyncProducer(cfg.KafkaBrokers, nil)
	if err != nil {
		log.Fatalf("Kafka producer error: %v", err)
	}
	defer producer.Close()

	deps := processorDependencies{
		incrementDemand: func(productID string, delta int64) (int64, error) {
			return rdb.IncrBy(ctx, demandKey(productID), delta).Result()
		},
		persistPrice: func(productID string, price float64) error {
			return rdb.Set(ctx, priceKey(productID), price, 0).Err()
		},
		persistHistory: db.InsertPriceHistory,
		publishUpdate: func(update PriceUpdate) error {
			data, err := json.Marshal(update)
			if err != nil {
				return fmt.Errorf("marshal update: %w", err)
			}

			msg := &sarama.ProducerMessage{
				Topic: cfg.PriceUpdatesTopic,
				Value: sarama.ByteEncoder(data),
			}

			if _, _, err := producer.SendMessage(msg); err != nil {
				return err
			}
			return nil
		},
		nowUnix: func() int64 {
			return timeNowUnix()
		},
		sleep: sleepWithDuration,
	}

	log.Printf("🚀 Pricing engine listening on topic %q...", cfg.ProductEventsTopic)

	for message := range partitionConsumer.Messages() {
		var event ProductEvent
		if err := json.Unmarshal(message.Value, &event); err == nil {
			skip, skipErr := shouldSkipEventForOverride(rdb, event.ProductID, event.Timestamp)
			if skipErr != nil {
				log.Printf("warning: override filter check failed for %s: %v", event.ProductID, skipErr)
			} else if skip {
				continue
			}
		}

		if err := processIncomingEvent(message.Value, deps); err != nil {
			log.Printf("❌ Event processing failed: %v", err)
		}
	}
}

func shouldSkipEventForOverride(rdb *redis.Client, productID string, eventTimestamp int64) (bool, error) {
	trimmedProductID := strings.TrimSpace(productID)
	if trimmedProductID == "" {
		return false, nil
	}

	activeValue, err := rdb.Get(ctx, overridestate.ActiveKey(trimmedProductID)).Result()
	if err != nil && err != redis.Nil {
		return false, err
	}
	if err == nil && strings.TrimSpace(activeValue) != "" {
		return true, nil
	}

	removedAtRaw, err := rdb.Get(ctx, overridestate.RemovedAtKey(trimmedProductID)).Result()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	removedAtMs, err := strconv.ParseInt(strings.TrimSpace(removedAtRaw), 10, 64)
	if err != nil {
		return false, fmt.Errorf("parse override removed timestamp: %w", err)
	}

	eventMs := normalizeEventTimestampMs(eventTimestamp)
	return eventMs <= removedAtMs, nil
}

func normalizeEventTimestampMs(value int64) int64 {
	if value >= 1_000_000_000_000 {
		return value
	}
	return value * 1000
}
