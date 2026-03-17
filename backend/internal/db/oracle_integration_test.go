package db

import (
	"os"
	"strings"
	"testing"
	"time"
)

const (
	oracleIntegrationEnv = "RUN_ORACLE_INTEGRATION_TESTS"
	defaultOracleDSN     = "pricing/pricingpass@localhost:1521/FREEPDB1"
	defaultOracleTZ      = "UTC"
)

func TestOracleOverrideLifecycleIntegration(t *testing.T) {
	if os.Getenv(oracleIntegrationEnv) != "1" {
		t.Skipf("skipping Oracle integration test; set %s=1 to enable", oracleIntegrationEnv)
	}

	if err := InitOracle(loadOracleDSN(), strings.TrimSpace(os.Getenv("ORACLE_LIB_DIR")), loadOracleTimezone()); err != nil {
		t.Fatalf("failed to init oracle for integration test: %v", err)
	}

	productID := "TOVR_" + time.Now().Format("20060102150405")

	deleted, err := DeletePriceOverride(productID)
	if err != nil {
		t.Fatalf("cleanup delete failed: %v", err)
	}
	if deleted {
		t.Fatalf("expected no pre-existing override for %s", productID)
	}

	if err := UpsertPriceOverride(productID, 150.25, "first"); err != nil {
		t.Fatalf("upsert insert failed: %v", err)
	}

	override, found, err := GetPriceOverride(productID)
	if err != nil {
		t.Fatalf("get override after insert failed: %v", err)
	}
	if !found {
		t.Fatalf("expected override to exist after insert")
	}
	if override.OverridePrice != 150.25 || override.Reason != "first" {
		t.Fatalf("unexpected override after insert: %#v", override)
	}

	if err := UpsertPriceOverride(productID, 199.99, "updated"); err != nil {
		t.Fatalf("upsert update failed: %v", err)
	}

	override, found, err = GetPriceOverride(productID)
	if err != nil {
		t.Fatalf("get override after update failed: %v", err)
	}
	if !found {
		t.Fatalf("expected override to exist after update")
	}
	if override.OverridePrice != 199.99 || override.Reason != "updated" {
		t.Fatalf("unexpected override after update: %#v", override)
	}

	deleted, err = DeletePriceOverride(productID)
	if err != nil {
		t.Fatalf("delete override failed: %v", err)
	}
	if !deleted {
		t.Fatalf("expected delete to report true")
	}

	deleted, err = DeletePriceOverride(productID)
	if err != nil {
		t.Fatalf("second delete override failed: %v", err)
	}
	if deleted {
		t.Fatalf("expected second delete to report false")
	}
}

func TestOraclePriceHistoryLimitAndOrderIntegration(t *testing.T) {
	if os.Getenv(oracleIntegrationEnv) != "1" {
		t.Skipf("skipping Oracle integration test; set %s=1 to enable", oracleIntegrationEnv)
	}

	if err := InitOracle(loadOracleDSN(), strings.TrimSpace(os.Getenv("ORACLE_LIB_DIR")), loadOracleTimezone()); err != nil {
		t.Fatalf("failed to init oracle for integration test: %v", err)
	}

	productID := "THST_" + time.Now().Format("20060102150405")

	if err := InsertPriceHistory(productID, 101.0); err != nil {
		t.Fatalf("insert history #1 failed: %v", err)
	}
	time.Sleep(15 * time.Millisecond)
	if err := InsertPriceHistory(productID, 102.0); err != nil {
		t.Fatalf("insert history #2 failed: %v", err)
	}
	time.Sleep(15 * time.Millisecond)
	if err := InsertPriceHistory(productID, 103.0); err != nil {
		t.Fatalf("insert history #3 failed: %v", err)
	}

	rows, err := ListPriceHistory(productID, 2)
	if err != nil {
		t.Fatalf("list history failed: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("unexpected row count: got %d, want 2", len(rows))
	}
	if rows[0].EventTime.Before(rows[1].EventTime) {
		t.Fatalf("history is not sorted descending by event_time: %#v", rows)
	}
	for _, row := range rows {
		if strings.TrimSpace(row.PriceSource) == "" {
			t.Fatalf("expected non-empty price source in row: %#v", row)
		}
	}
}

func loadOracleDSN() string {
	dsn := strings.TrimSpace(os.Getenv("ORACLE_DSN"))
	if dsn == "" {
		return defaultOracleDSN
	}
	return dsn
}

func loadOracleTimezone() string {
	timezone := strings.TrimSpace(os.Getenv("ORACLE_TIMEZONE"))
	if timezone == "" {
		return defaultOracleTZ
	}
	return timezone
}
