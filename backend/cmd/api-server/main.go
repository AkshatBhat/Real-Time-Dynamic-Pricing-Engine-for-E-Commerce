package main

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"dynamic-pricing-engine/backend/internal/config"
	"dynamic-pricing-engine/backend/internal/db"
	"dynamic-pricing-engine/backend/internal/overridestate"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"
)

var (
	ctx              = context.Background()
	cacheClient      redisStringGetter
	priceStore       overrideHistoryStore
	adminAPIKey      string
	apiKeyHeader     string
	validProductIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
)

const (
	defaultHistoryLimit   = 50
	maxHistoryLimit       = 500
	defaultOverrideReason = "manual override"
)

type redisStringGetter interface {
	Get(ctx context.Context, key string) *redis.StringCmd
	Set(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.StatusCmd
	Del(ctx context.Context, keys ...string) *redis.IntCmd
}

type overrideHistoryStore interface {
	GetPriceOverride(productID string) (db.PriceOverride, bool, error)
	UpsertPriceOverride(productID string, overridePrice float64, reason string) error
	DeletePriceOverride(productID string) (bool, error)
	ListPriceHistory(productID string, limit int) ([]db.PriceHistoryRow, error)
	InsertPriceHistoryWithSource(productID string, price float64, source string) error
}

type oracleStore struct{}

func (oracleStore) GetPriceOverride(productID string) (db.PriceOverride, bool, error) {
	return db.GetPriceOverride(productID)
}

func (oracleStore) UpsertPriceOverride(productID string, overridePrice float64, reason string) error {
	return db.UpsertPriceOverride(productID, overridePrice, reason)
}

func (oracleStore) DeletePriceOverride(productID string) (bool, error) {
	return db.DeletePriceOverride(productID)
}

func (oracleStore) ListPriceHistory(productID string, limit int) ([]db.PriceHistoryRow, error) {
	return db.ListPriceHistory(productID, limit)
}

func (oracleStore) InsertPriceHistoryWithSource(productID string, price float64, source string) error {
	return db.InsertPriceHistoryWithSource(productID, price, source)
}

type PriceResponse struct {
	ProductID   string  `json:"product_id"`
	Price       float64 `json:"price"`
	Demand      int     `json:"demand"`
	PriceSource string  `json:"price_source"`
}

type OverrideRequest struct {
	ProductID     string  `json:"product_id"`
	OverridePrice float64 `json:"override_price"`
	Reason        string  `json:"reason"`
}

type OverrideResponse struct {
	ProductID     string  `json:"product_id"`
	OverridePrice float64 `json:"override_price"`
	Reason        string  `json:"reason"`
	Status        string  `json:"status"`
}

type DeleteOverrideResponse struct {
	ProductID string `json:"product_id"`
	Deleted   bool   `json:"deleted"`
}

type PriceHistoryEntry struct {
	Price       float64   `json:"price"`
	EventTime   time.Time `json:"event_time"`
	PriceSource string    `json:"price_source"`
}

type PriceHistoryResponse struct {
	ProductID string              `json:"product_id"`
	History   []PriceHistoryEntry `json:"history"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

func main() {
	cfg, err := config.LoadAPIConfig()
	if err != nil {
		log.Fatalf("Invalid configuration: %v", err)
	}

	cacheClient = redis.NewClient(&redis.Options{
		Addr: cfg.RedisAddr,
	})
	if err := db.InitOracle(cfg.OracleDSN, cfg.OracleLibDir, cfg.OracleTimezone); err != nil {
		log.Fatalf("Oracle init failed: %v", err)
	}
	priceStore = oracleStore{}
	adminAPIKey = cfg.AdminAPIKey
	apiKeyHeader = cfg.APIKeyHeader

	hub := newPriceUpdateHub()
	go hub.run()
	defer hub.shutdown()

	priceUpdatesConsumer, priceUpdatesPartitionConsumer, err := startPriceUpdatesFanout(cfg.KafkaBrokers, cfg.PriceUpdatesTopic, hub)
	if err != nil {
		log.Fatalf("Kafka price-updates fanout init failed: %v", err)
	}
	defer priceUpdatesConsumer.Close()
	defer priceUpdatesPartitionConsumer.Close()

	r := chi.NewRouter()
	r.Get("/ws/prices", wsPricesHandler(hub))
	r.Get("/price/{productID}", getPriceHandler)
	r.Group(func(group chi.Router) {
		group.Use(requireAdminAPIKey)
		group.Post("/override", postOverrideHandler)
		group.Delete("/override/{productID}", deleteOverrideHandler)
		group.Get("/history/{productID}", getHistoryHandler)
	})

	addr := fmt.Sprintf(":%d", cfg.Port)
	log.Printf("🚀 API server running on http://localhost%s", addr)
	if err := http.ListenAndServe(addr, r); err != nil {
		log.Fatalf("API server stopped: %v", err)
	}
}

func getPriceHandler(w http.ResponseWriter, r *http.Request) {
	productID := chi.URLParam(r, "productID")
	if !validProductIDRe.MatchString(productID) {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid productID"})
		return
	}

	override, hasOverride, err := priceStore.GetPriceOverride(productID)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, ErrorResponse{Error: "override lookup unavailable"})
		return
	}

	demandStr, err := getCacheString(productDemandKey(productID))
	if err != nil {
		writeCacheError(w, "demand", err)
		return
	}
	demand, err := strconv.Atoi(demandStr)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "cached demand is malformed"})
		return
	}

	var price float64
	priceSource := "dynamic"
	if hasOverride {
		price = override.OverridePrice
		priceSource = "override"
	} else {
		priceStr, err := getCacheString(productPriceKey(productID))
		if err != nil {
			writeCacheError(w, "price", err)
			return
		}
		price, err = strconv.ParseFloat(priceStr, 64)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "cached price is malformed"})
			return
		}
	}

	resp := PriceResponse{
		ProductID:   productID,
		Price:       price,
		Demand:      demand,
		PriceSource: priceSource,
	}
	writeJSON(w, http.StatusOK, resp)
}

func postOverrideHandler(w http.ResponseWriter, r *http.Request) {
	var req OverrideRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
		return
	}

	productID := strings.TrimSpace(req.ProductID)
	if !validProductIDRe.MatchString(productID) {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid product_id"})
		return
	}
	if req.OverridePrice <= 0 {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "override_price must be greater than 0"})
		return
	}

	if err := ensureProductExists(productID); err != nil {
		switch {
		case errors.Is(err, errProductNotFound):
			writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "product not found"})
		case errors.Is(err, errCacheUnavailable):
			writeJSON(w, http.StatusBadGateway, ErrorResponse{Error: "cache unavailable"})
		default:
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "failed to validate product existence"})
		}
		return
	}

	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		reason = defaultOverrideReason
	}

	if err := priceStore.UpsertPriceOverride(productID, req.OverridePrice, reason); err != nil {
		writeJSON(w, http.StatusBadGateway, ErrorResponse{Error: "override store unavailable"})
		return
	}
	markOverrideActive(productID)
	if err := setDynamicPriceForProduct(productID, req.OverridePrice); err != nil {
		log.Printf("warning: failed to sync Redis price to override for %s: %v", productID, err)
	}
	appendHistoryPoint(productID, req.OverridePrice, "override")

	writeJSON(w, http.StatusOK, OverrideResponse{
		ProductID:     productID,
		OverridePrice: req.OverridePrice,
		Reason:        reason,
		Status:        "upserted",
	})
}

func deleteOverrideHandler(w http.ResponseWriter, r *http.Request) {
	productID := chi.URLParam(r, "productID")
	if !validProductIDRe.MatchString(productID) {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid productID"})
		return
	}

	deleted, err := priceStore.DeletePriceOverride(productID)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, ErrorResponse{Error: "override store unavailable"})
		return
	}
	if !deleted {
		writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "override not found"})
		return
	}
	markOverrideRemoved(productID)

	writeJSON(w, http.StatusOK, DeleteOverrideResponse{
		ProductID: productID,
		Deleted:   true,
	})
}

func getHistoryHandler(w http.ResponseWriter, r *http.Request) {
	productID := chi.URLParam(r, "productID")
	if !validProductIDRe.MatchString(productID) {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid productID"})
		return
	}

	if err := ensureProductExists(productID); err != nil {
		switch {
		case errors.Is(err, errProductNotFound):
			writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "product not found"})
		case errors.Is(err, errCacheUnavailable):
			writeJSON(w, http.StatusBadGateway, ErrorResponse{Error: "cache unavailable"})
		default:
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "failed to validate product existence"})
		}
		return
	}

	limit, err := parseHistoryLimit(r.URL.Query().Get("limit"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	rows, err := priceStore.ListPriceHistory(productID, limit)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, ErrorResponse{Error: "history store unavailable"})
		return
	}

	history := make([]PriceHistoryEntry, 0, len(rows))
	for _, row := range rows {
		priceSource := strings.TrimSpace(row.PriceSource)
		if priceSource == "" {
			priceSource = "dynamic"
		}
		history = append(history, PriceHistoryEntry{
			Price:       row.Price,
			EventTime:   row.EventTime,
			PriceSource: priceSource,
		})
	}

	writeJSON(w, http.StatusOK, PriceHistoryResponse{
		ProductID: productID,
		History:   history,
	})
}

func getCacheString(key string) (string, error) {
	return cacheClient.Get(ctx, key).Result()
}

func requireAdminAPIKey(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provided := strings.TrimSpace(r.Header.Get(apiKeyHeader))
		if provided == "" || !apiKeyAuthorized(provided) {
			writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "unauthorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func apiKeyAuthorized(provided string) bool {
	providedHash := sha256.Sum256([]byte(provided))
	expectedHash := sha256.Sum256([]byte(adminAPIKey))
	return subtle.ConstantTimeCompare(providedHash[:], expectedHash[:]) == 1
}

func writeCacheError(w http.ResponseWriter, field string, err error) {
	if errors.Is(err, redis.Nil) {
		writeJSON(w, http.StatusNotFound, ErrorResponse{Error: fmt.Sprintf("%s not found", field)})
		return
	}

	writeJSON(w, http.StatusBadGateway, ErrorResponse{Error: "cache unavailable"})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("Failed to write response: %v", err)
	}
}

func decodeJSONBody(r *http.Request, dst any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}

	var trailingToken json.RawMessage
	if err := decoder.Decode(&trailingToken); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}

	return errors.New("request body must contain a single JSON object")
}

func parseHistoryLimit(raw string) (int, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return defaultHistoryLimit, nil
	}

	limit, err := strconv.Atoi(value)
	if err != nil {
		return 0, errors.New("invalid limit query parameter")
	}
	if limit <= 0 {
		return 0, errors.New("limit must be greater than 0")
	}
	if limit > maxHistoryLimit {
		return maxHistoryLimit, nil
	}
	return limit, nil
}

func productPriceKey(productID string) string {
	return fmt.Sprintf("product:price:%s", productID)
}

func productDemandKey(productID string) string {
	return fmt.Sprintf("product:demand:%s", productID)
}

var (
	errProductNotFound  = errors.New("product not found")
	errCacheUnavailable = errors.New("cache unavailable")
)

func ensureProductExists(productID string) error {
	_, demandErr := getCacheString(productDemandKey(productID))
	if demandErr == nil {
		return nil
	}

	_, priceErr := getCacheString(productPriceKey(productID))
	if priceErr == nil {
		return nil
	}

	if errors.Is(demandErr, redis.Nil) && errors.Is(priceErr, redis.Nil) {
		return errProductNotFound
	}

	return errCacheUnavailable
}

func appendHistoryPoint(productID string, price float64, source string) {
	if err := priceStore.InsertPriceHistoryWithSource(productID, price, source); err != nil {
		log.Printf("warning: history append failed for %s (%s): %v", productID, source, err)
	}
}

func setDynamicPriceForProduct(productID string, price float64) error {
	return cacheClient.Set(ctx, productPriceKey(productID), price, 0).Err()
}

func markOverrideActive(productID string) {
	if err := cacheClient.Set(ctx, overridestate.ActiveKey(productID), "1", 0).Err(); err != nil {
		log.Printf("warning: failed to mark override active for %s: %v", productID, err)
	}
}

func markOverrideRemoved(productID string) {
	if err := cacheClient.Del(ctx, overridestate.ActiveKey(productID)).Err(); err != nil {
		log.Printf("warning: failed to clear override active flag for %s: %v", productID, err)
	}
	if err := cacheClient.Set(ctx, overridestate.RemovedAtKey(productID), time.Now().UnixMilli(), 0).Err(); err != nil {
		log.Printf("warning: failed to set override removed_at for %s: %v", productID, err)
	}
}
