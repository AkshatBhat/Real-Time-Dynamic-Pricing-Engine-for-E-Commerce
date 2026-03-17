package main

import (
	"context"
	"dynamic-pricing-engine/backend/internal/db"
	"dynamic-pricing-engine/backend/internal/overridestate"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"
)

const (
	testAdminAPIKey = "phase4-test-admin-key"
	testAPIKeyHdr   = "X-API-Key"
)

type fakeRedisClient struct {
	values map[string]redisResult
}

type redisResult struct {
	value string
	err   error
}

func (f *fakeRedisClient) Get(ctx context.Context, key string) *redis.StringCmd {
	result, ok := f.values[key]
	if !ok {
		return redis.NewStringResult("", redis.Nil)
	}
	return redis.NewStringResult(result.value, result.err)
}

func (f *fakeRedisClient) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.StatusCmd {
	if f.values == nil {
		f.values = map[string]redisResult{}
	}
	f.values[key] = redisResult{
		value: fmt.Sprint(value),
		err:   nil,
	}
	return redis.NewStatusResult("OK", nil)
}

func (f *fakeRedisClient) Del(ctx context.Context, keys ...string) *redis.IntCmd {
	for _, key := range keys {
		delete(f.values, key)
	}
	return redis.NewIntResult(int64(len(keys)), nil)
}

type fakeStore struct {
	overrideByProduct map[string]db.PriceOverride
	getOverrideErr    error
	upsertErr         error
	deleteErr         error
	deleteOutcome     map[string]bool
	historyByProduct  map[string][]db.PriceHistoryRow
	historyErr        error
	insertHistoryErr  error

	lastUpsertProduct string
	lastUpsertPrice   float64
	lastUpsertReason  string
	lastHistoryLimit  int
	lastHistoryID     string
	lastInsertProduct string
	lastInsertPrice   float64
	lastInsertSource  string
}

func (f *fakeStore) GetPriceOverride(productID string) (db.PriceOverride, bool, error) {
	if f.getOverrideErr != nil {
		return db.PriceOverride{}, false, f.getOverrideErr
	}
	override, ok := f.overrideByProduct[productID]
	if !ok {
		return db.PriceOverride{}, false, nil
	}
	return override, true, nil
}

func (f *fakeStore) UpsertPriceOverride(productID string, overridePrice float64, reason string) error {
	if f.upsertErr != nil {
		return f.upsertErr
	}
	f.lastUpsertProduct = productID
	f.lastUpsertPrice = overridePrice
	f.lastUpsertReason = reason
	if f.overrideByProduct == nil {
		f.overrideByProduct = map[string]db.PriceOverride{}
	}
	f.overrideByProduct[productID] = db.PriceOverride{
		ProductID:     productID,
		OverridePrice: overridePrice,
		Reason:        reason,
		SetTime:       time.Now(),
	}
	return nil
}

func (f *fakeStore) DeletePriceOverride(productID string) (bool, error) {
	if f.deleteErr != nil {
		return false, f.deleteErr
	}
	if f.deleteOutcome != nil {
		return f.deleteOutcome[productID], nil
	}
	if f.overrideByProduct == nil {
		return false, nil
	}
	if _, ok := f.overrideByProduct[productID]; !ok {
		return false, nil
	}
	delete(f.overrideByProduct, productID)
	return true, nil
}

func (f *fakeStore) ListPriceHistory(productID string, limit int) ([]db.PriceHistoryRow, error) {
	f.lastHistoryID = productID
	f.lastHistoryLimit = limit
	if f.historyErr != nil {
		return nil, f.historyErr
	}
	return f.historyByProduct[productID], nil
}

func (f *fakeStore) InsertPriceHistoryWithSource(productID string, price float64, source string) error {
	if f.insertHistoryErr != nil {
		return f.insertHistoryErr
	}
	f.lastInsertProduct = productID
	f.lastInsertPrice = price
	f.lastInsertSource = source
	return nil
}

func setupRouterForTests() http.Handler {
	adminAPIKey = testAdminAPIKey
	apiKeyHeader = testAPIKeyHdr

	r := chi.NewRouter()
	r.Get("/price/{productID}", getPriceHandler)
	r.Group(func(group chi.Router) {
		group.Use(requireAdminAPIKey)
		group.Post("/override", postOverrideHandler)
		group.Delete("/override/{productID}", deleteOverrideHandler)
		group.Get("/history/{productID}", getHistoryHandler)
	})
	return r
}

func withAuth(req *http.Request) *http.Request {
	req.Header.Set(testAPIKeyHdr, testAdminAPIKey)
	return req
}

func TestGetPriceHandlerDynamicPrice(t *testing.T) {
	cacheClient = &fakeRedisClient{
		values: map[string]redisResult{
			productPriceKey("PA"):  {value: "123.45"},
			productDemandKey("PA"): {value: "7"},
		},
	}
	priceStore = &fakeStore{}

	req := httptest.NewRequest(http.MethodGet, "/price/PA", nil)
	rec := httptest.NewRecorder()

	setupRouterForTests().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status code: got %d, want %d", rec.Code, http.StatusOK)
	}

	var response PriceResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to parse response JSON: %v", err)
	}

	if response.ProductID != "PA" || response.Price != 123.45 || response.Demand != 7 {
		t.Fatalf("unexpected response payload: %#v", response)
	}
	if response.PriceSource != "dynamic" {
		t.Fatalf("unexpected price source: %q", response.PriceSource)
	}
}

func TestGetPriceHandlerOverridePrice(t *testing.T) {
	cacheClient = &fakeRedisClient{
		values: map[string]redisResult{
			productDemandKey("PA"): {value: "9"},
		},
	}
	priceStore = &fakeStore{
		overrideByProduct: map[string]db.PriceOverride{
			"PA": {
				ProductID:     "PA",
				OverridePrice: 188.75,
				Reason:        "manual",
				SetTime:       time.Now(),
			},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/price/PA", nil)
	rec := httptest.NewRecorder()

	setupRouterForTests().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status code: got %d, want %d", rec.Code, http.StatusOK)
	}

	var response PriceResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to parse response JSON: %v", err)
	}
	if response.Price != 188.75 || response.PriceSource != "override" {
		t.Fatalf("unexpected override response: %#v", response)
	}
}

func TestGetPriceHandlerOverrideLookupFailure(t *testing.T) {
	cacheClient = &fakeRedisClient{
		values: map[string]redisResult{
			productPriceKey("PA"):  {value: "123.45"},
			productDemandKey("PA"): {value: "7"},
		},
	}
	priceStore = &fakeStore{
		getOverrideErr: errors.New("oracle unavailable"),
	}

	req := httptest.NewRequest(http.MethodGet, "/price/PA", nil)
	rec := httptest.NewRecorder()

	setupRouterForTests().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("unexpected status code: got %d, want %d", rec.Code, http.StatusBadGateway)
	}
}

func TestPostOverrideHandlerHappyPath(t *testing.T) {
	fakeCache := &fakeRedisClient{
		values: map[string]redisResult{
			productDemandKey("PA"): {value: "3"},
		},
	}
	cacheClient = fakeCache
	store := &fakeStore{}
	priceStore = store

	body := `{"product_id":"PA","override_price":149.99,"reason":"campaign"}`
	req := withAuth(httptest.NewRequest(http.MethodPost, "/override", strings.NewReader(body)))
	rec := httptest.NewRecorder()

	setupRouterForTests().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status code: got %d, want %d", rec.Code, http.StatusOK)
	}
	if store.lastUpsertProduct != "PA" || store.lastUpsertPrice != 149.99 || store.lastUpsertReason != "campaign" {
		t.Fatalf("unexpected upsert values: product=%q price=%f reason=%q", store.lastUpsertProduct, store.lastUpsertPrice, store.lastUpsertReason)
	}
	if store.lastInsertProduct != "PA" || store.lastInsertSource != "override" || store.lastInsertPrice != 149.99 {
		t.Fatalf("unexpected history insert values: product=%q source=%q price=%f", store.lastInsertProduct, store.lastInsertSource, store.lastInsertPrice)
	}
	if fakeCache.values[productPriceKey("PA")].value != "149.99" {
		t.Fatalf("expected redis price to be synced to override, got %q", fakeCache.values[productPriceKey("PA")].value)
	}
}

func TestPostOverrideHandlerProductNotFound(t *testing.T) {
	cacheClient = &fakeRedisClient{values: map[string]redisResult{}}
	priceStore = &fakeStore{}

	body := `{"product_id":"PA","override_price":149.99,"reason":"campaign"}`
	req := withAuth(httptest.NewRequest(http.MethodPost, "/override", strings.NewReader(body)))
	rec := httptest.NewRecorder()

	setupRouterForTests().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("unexpected status code: got %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestPostOverrideHandlerInvalidPayload(t *testing.T) {
	cacheClient = &fakeRedisClient{values: map[string]redisResult{}}
	priceStore = &fakeStore{}

	req := withAuth(httptest.NewRequest(http.MethodPost, "/override", strings.NewReader(`{"product_id":"PA","override_price":"bad"}`)))
	rec := httptest.NewRecorder()

	setupRouterForTests().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status code: got %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestPostOverrideHandlerInvalidProductID(t *testing.T) {
	cacheClient = &fakeRedisClient{values: map[string]redisResult{}}
	priceStore = &fakeStore{}

	req := withAuth(httptest.NewRequest(http.MethodPost, "/override", strings.NewReader(`{"product_id":"PA$","override_price":150}`)))
	rec := httptest.NewRecorder()

	setupRouterForTests().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status code: got %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestPostOverrideHandlerInvalidPrice(t *testing.T) {
	cacheClient = &fakeRedisClient{values: map[string]redisResult{}}
	priceStore = &fakeStore{}

	req := withAuth(httptest.NewRequest(http.MethodPost, "/override", strings.NewReader(`{"product_id":"PA","override_price":0}`)))
	rec := httptest.NewRecorder()

	setupRouterForTests().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status code: got %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestDeleteOverrideHandlerSuccess(t *testing.T) {
	fakeCache := &fakeRedisClient{
		values: map[string]redisResult{
			productPriceKey("PA"): {value: "111.50"},
		},
	}
	cacheClient = fakeCache
	store := &fakeStore{
		deleteOutcome: map[string]bool{
			"PA": true,
		},
	}
	priceStore = store

	req := withAuth(httptest.NewRequest(http.MethodDelete, "/override/PA", nil))
	rec := httptest.NewRecorder()

	setupRouterForTests().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status code: got %d, want %d", rec.Code, http.StatusOK)
	}
	if store.lastInsertProduct != "" {
		t.Fatalf("did not expect history insert on delete, got product=%q source=%q", store.lastInsertProduct, store.lastInsertSource)
	}
	if _, exists := fakeCache.values[overridestate.ActiveKey("PA")]; exists {
		t.Fatalf("expected override active key to be deleted")
	}
	if _, exists := fakeCache.values[overridestate.RemovedAtKey("PA")]; !exists {
		t.Fatalf("expected override removed_at key to be set")
	}
}

func TestDeleteOverrideHandlerNotFound(t *testing.T) {
	cacheClient = &fakeRedisClient{values: map[string]redisResult{}}
	priceStore = &fakeStore{
		deleteOutcome: map[string]bool{
			"PA": false,
		},
	}

	req := withAuth(httptest.NewRequest(http.MethodDelete, "/override/PA", nil))
	rec := httptest.NewRecorder()

	setupRouterForTests().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("unexpected status code: got %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestGetHistoryHandlerDefaultLimit(t *testing.T) {
	cacheClient = &fakeRedisClient{
		values: map[string]redisResult{
			productDemandKey("PA"): {value: "3"},
		},
	}
	store := &fakeStore{
		historyByProduct: map[string][]db.PriceHistoryRow{
			"PA": {
				{
					ProductID:   "PA",
					Price:       123.45,
					EventTime:   time.Now(),
					PriceSource: "base",
				},
			},
		},
	}
	priceStore = store

	req := withAuth(httptest.NewRequest(http.MethodGet, "/history/PA", nil))
	rec := httptest.NewRecorder()

	setupRouterForTests().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status code: got %d, want %d", rec.Code, http.StatusOK)
	}
	if store.lastHistoryLimit != defaultHistoryLimit {
		t.Fatalf("unexpected history limit: got %d, want %d", store.lastHistoryLimit, defaultHistoryLimit)
	}

	var response PriceHistoryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to parse response JSON: %v", err)
	}
	if response.History == nil {
		t.Fatalf("expected non-nil history array")
	}
	if len(response.History) != 1 {
		t.Fatalf("expected one history row, got %d", len(response.History))
	}
	if response.History[0].PriceSource != "base" {
		t.Fatalf("unexpected history price_source: %q", response.History[0].PriceSource)
	}
}

func TestGetHistoryHandlerCustomLimitClamped(t *testing.T) {
	cacheClient = &fakeRedisClient{
		values: map[string]redisResult{
			productDemandKey("PA"): {value: "3"},
		},
	}
	store := &fakeStore{
		historyByProduct: map[string][]db.PriceHistoryRow{
			"PA": {},
		},
	}
	priceStore = store

	req := withAuth(httptest.NewRequest(http.MethodGet, "/history/PA?limit=999", nil))
	rec := httptest.NewRecorder()

	setupRouterForTests().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status code: got %d, want %d", rec.Code, http.StatusOK)
	}
	if store.lastHistoryLimit != maxHistoryLimit {
		t.Fatalf("unexpected clamped limit: got %d, want %d", store.lastHistoryLimit, maxHistoryLimit)
	}
}

func TestGetHistoryHandlerInvalidLimit(t *testing.T) {
	cacheClient = &fakeRedisClient{
		values: map[string]redisResult{
			productDemandKey("PA"): {value: "3"},
		},
	}
	priceStore = &fakeStore{}

	req := withAuth(httptest.NewRequest(http.MethodGet, "/history/PA?limit=abc", nil))
	rec := httptest.NewRecorder()

	setupRouterForTests().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status code: got %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestGetHistoryHandlerProductNotFound(t *testing.T) {
	cacheClient = &fakeRedisClient{values: map[string]redisResult{}}
	priceStore = &fakeStore{}

	req := withAuth(httptest.NewRequest(http.MethodGet, "/history/PA", nil))
	rec := httptest.NewRecorder()

	setupRouterForTests().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("unexpected status code: got %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestProtectedRoutesMissingAPIKey(t *testing.T) {
	cacheClient = &fakeRedisClient{
		values: map[string]redisResult{
			productDemandKey("PA"): {value: "3"},
			productPriceKey("PA"):  {value: "100"},
		},
	}
	priceStore = &fakeStore{
		deleteOutcome: map[string]bool{
			"PA": true,
		},
	}

	tests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{
			name:   "post override",
			method: http.MethodPost,
			path:   "/override",
			body:   `{"product_id":"PA","override_price":150}`,
		},
		{
			name:   "delete override",
			method: http.MethodDelete,
			path:   "/override/PA",
		},
		{
			name:   "get history",
			method: http.MethodGet,
			path:   "/history/PA",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			rec := httptest.NewRecorder()

			setupRouterForTests().ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("unexpected status code: got %d, want %d", rec.Code, http.StatusUnauthorized)
			}
		})
	}
}

func TestProtectedRoutesInvalidAPIKey(t *testing.T) {
	cacheClient = &fakeRedisClient{
		values: map[string]redisResult{
			productDemandKey("PA"): {value: "3"},
			productPriceKey("PA"):  {value: "100"},
		},
	}
	priceStore = &fakeStore{
		deleteOutcome: map[string]bool{
			"PA": true,
		},
	}

	tests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{
			name:   "post override",
			method: http.MethodPost,
			path:   "/override",
			body:   `{"product_id":"PA","override_price":150}`,
		},
		{
			name:   "delete override",
			method: http.MethodDelete,
			path:   "/override/PA",
		},
		{
			name:   "get history",
			method: http.MethodGet,
			path:   "/history/PA",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req.Header.Set(testAPIKeyHdr, "invalid-test-key")
			rec := httptest.NewRecorder()

			setupRouterForTests().ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("unexpected status code: got %d, want %d", rec.Code, http.StatusUnauthorized)
			}
		})
	}
}
