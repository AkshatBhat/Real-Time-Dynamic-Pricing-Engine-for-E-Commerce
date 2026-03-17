package db

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	_ "github.com/godror/godror"
)

var Oracle *sql.DB

type PriceOverride struct {
	ProductID     string
	OverridePrice float64
	Reason        string
	SetTime       time.Time
}

type PriceHistoryRow struct {
	ProductID   string
	Price       float64
	EventTime   time.Time
	PriceSource string
}

func InitOracle(dsn, libDir, timezone string) error {
	connectionString, err := buildConnectionString(dsn, libDir, timezone)
	if err != nil {
		return fmt.Errorf("invalid oracle configuration: %w", err)
	}

	Oracle, err = sql.Open("godror", connectionString)
	if err != nil {
		return fmt.Errorf("failed to create Oracle connection: %w", err)
	}
	// Local stability guard: avoid concurrent OCI connection creation, which can
	// be flaky on some macOS + Instant Client setups.
	Oracle.SetMaxOpenConns(1)
	Oracle.SetMaxIdleConns(1)
	Oracle.SetConnMaxLifetime(30 * time.Minute)

	if err = Oracle.Ping(); err != nil {
		return fmt.Errorf("oracle ping failed: %w", err)
	}

	log.Println("✅ Connected to Oracle DB")
	return nil
}

func InsertPriceHistory(productID string, price float64) error {
	return InsertPriceHistoryWithSource(productID, price, "dynamic")
}

func InsertPriceHistoryWithSource(productID string, price float64, source string) error {
	if err := ensureOracleInitialized(); err != nil {
		return err
	}

	if source == "" {
		source = "dynamic"
	}

	stmt := `INSERT INTO price_history (product_id, price, price_source) VALUES (:1, :2, :3)`
	_, err := Oracle.Exec(stmt, productID, price, source)
	if err != nil {
		return fmt.Errorf("oracle insert failed for %s: %w", productID, err)
	}
	log.Printf("📝 Inserted into price_history: %s → %.2f (%s)", productID, price, source)
	return nil
}

func HasPriceHistory(productID string) (bool, error) {
	if err := ensureOracleInitialized(); err != nil {
		return false, err
	}

	var count int
	stmt := `SELECT COUNT(1) FROM price_history WHERE product_id = :1`
	if err := Oracle.QueryRow(stmt, productID).Scan(&count); err != nil {
		return false, fmt.Errorf("oracle history existence check failed for %s: %w", productID, err)
	}

	return count > 0, nil
}

func EnsureBasePriceHistory(productID string, basePrice float64) error {
	if err := ensureOracleInitialized(); err != nil {
		return err
	}

	var baseCount int
	baseCountStmt := `
SELECT COUNT(1)
FROM price_history
WHERE product_id = :1
  AND NVL(price_source, 'dynamic') = 'base'
`
	if err := Oracle.QueryRow(baseCountStmt, productID).Scan(&baseCount); err != nil {
		return fmt.Errorf("oracle base history existence check failed for %s: %w", productID, err)
	}
	if baseCount > 0 {
		return nil
	}

	var earliestEventTime sql.NullTime
	minEventStmt := `SELECT MIN(event_time) FROM price_history WHERE product_id = :1`
	if err := Oracle.QueryRow(minEventStmt, productID).Scan(&earliestEventTime); err != nil {
		return fmt.Errorf("oracle earliest history lookup failed for %s: %w", productID, err)
	}

	if earliestEventTime.Valid {
		stmt := `
INSERT INTO price_history (product_id, price, price_source, event_time)
VALUES (:1, :2, :3, :4)
`
		seedTime := earliestEventTime.Time.Add(-1 * time.Millisecond)
		if _, err := Oracle.Exec(stmt, productID, basePrice, "base", seedTime); err != nil {
			return fmt.Errorf("oracle baseline history backfill failed for %s: %w", productID, err)
		}
		return nil
	}

	return InsertPriceHistoryWithSource(productID, basePrice, "base")
}

func GetPriceOverride(productID string) (PriceOverride, bool, error) {
	if err := ensureOracleInitialized(); err != nil {
		return PriceOverride{}, false, err
	}

	stmt := `SELECT product_id, override_price, reason, set_time FROM price_overrides WHERE product_id = :1`
	var override PriceOverride
	var reason sql.NullString
	err := Oracle.QueryRow(stmt, productID).Scan(
		&override.ProductID,
		&override.OverridePrice,
		&reason,
		&override.SetTime,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return PriceOverride{}, false, nil
		}
		return PriceOverride{}, false, fmt.Errorf("oracle override read failed for %s: %w", productID, err)
	}
	override.Reason = reason.String

	return override, true, nil
}

func UpsertPriceOverride(productID string, overridePrice float64, reason string) error {
	if err := ensureOracleInitialized(); err != nil {
		return err
	}

	stmt := `
MERGE INTO price_overrides po
USING (SELECT :1 AS product_id, :2 AS override_price, :3 AS reason FROM dual) incoming
ON (po.product_id = incoming.product_id)
WHEN MATCHED THEN
  UPDATE SET
    po.override_price = incoming.override_price,
    po.reason = incoming.reason,
    po.set_time = CURRENT_TIMESTAMP
WHEN NOT MATCHED THEN
  INSERT (product_id, override_price, reason, set_time)
  VALUES (incoming.product_id, incoming.override_price, incoming.reason, CURRENT_TIMESTAMP)
`

	if _, err := Oracle.Exec(stmt, productID, overridePrice, reason); err != nil {
		return fmt.Errorf("oracle override upsert failed for %s: %w", productID, err)
	}
	return nil
}

func DeletePriceOverride(productID string) (bool, error) {
	if err := ensureOracleInitialized(); err != nil {
		return false, err
	}

	result, err := Oracle.Exec(`DELETE FROM price_overrides WHERE product_id = :1`, productID)
	if err != nil {
		return false, fmt.Errorf("oracle override delete failed for %s: %w", productID, err)
	}

	rowsDeleted, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("oracle override delete rows check failed for %s: %w", productID, err)
	}

	return rowsDeleted > 0, nil
}

func ListPriceHistory(productID string, limit int) ([]PriceHistoryRow, error) {
	if err := ensureOracleInitialized(); err != nil {
		return nil, err
	}
	if limit <= 0 {
		return nil, errors.New("limit must be greater than 0")
	}

	history := make([]PriceHistoryRow, 0, limit)
	stmt := `
SELECT product_id, price, event_time, NVL(price_source, 'dynamic')
FROM (
  SELECT product_id, price, event_time, price_source
  FROM price_history
  WHERE product_id = :1
  ORDER BY event_time DESC
)
WHERE ROWNUM <= :2
ORDER BY event_time DESC
`
	rows, err := Oracle.Query(stmt, productID, limit)
	if err != nil {
		return nil, fmt.Errorf("oracle price history query failed for %s: %w", productID, err)
	}
	defer rows.Close()

	for rows.Next() {
		var row PriceHistoryRow
		if err := rows.Scan(&row.ProductID, &row.Price, &row.EventTime, &row.PriceSource); err != nil {
			return nil, fmt.Errorf("oracle price history scan failed for %s: %w", productID, err)
		}
		history = append(history, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("oracle price history rows iteration failed for %s: %w", productID, err)
	}

	return history, nil
}

func buildConnectionString(dsn, libDir, timezone string) (string, error) {
	trimmedDSN := strings.TrimSpace(dsn)
	if trimmedDSN == "" {
		return "", errors.New("ORACLE_DSN cannot be empty")
	}

	trimmedLibDir := strings.TrimSpace(libDir)
	trimmedTimezone := strings.TrimSpace(timezone)
	if trimmedTimezone == "" {
		trimmedTimezone = "UTC"
	}

	connectionString := trimmedDSN
	if !(containsIgnoreCase(trimmedDSN, "user=") || containsIgnoreCase(trimmedDSN, "connectString=")) {
		user, password, connectString, err := parseEasyConnectDSN(trimmedDSN)
		if err != nil {
			return "", err
		}

		connectionString = fmt.Sprintf(
			`user="%s" password="%s" connectString="%s"`,
			escapeDoubleQuotes(user),
			escapeDoubleQuotes(password),
			escapeDoubleQuotes(connectString),
		)
	}

	if trimmedLibDir != "" && !containsIgnoreCase(connectionString, "libDir=") {
		connectionString += fmt.Sprintf(` libDir="%s"`, escapeDoubleQuotes(trimmedLibDir))
	}

	if trimmedTimezone != "" && !containsIgnoreCase(connectionString, "timezone=") {
		connectionString += fmt.Sprintf(` timezone="%s"`, escapeDoubleQuotes(trimmedTimezone))
	}

	return connectionString, nil
}

func parseEasyConnectDSN(dsn string) (string, string, string, error) {
	slashIndex := strings.Index(dsn, "/")
	atIndex := strings.LastIndex(dsn, "@")
	if slashIndex <= 0 || atIndex <= slashIndex+1 || atIndex >= len(dsn)-1 {
		return "", "", "", errors.New("ORACLE_DSN must use format user/password@host:port/service when ORACLE_LIB_DIR is set")
	}

	user := strings.TrimSpace(dsn[:slashIndex])
	password := strings.TrimSpace(dsn[slashIndex+1 : atIndex])
	connectString := strings.TrimSpace(dsn[atIndex+1:])
	if user == "" || password == "" || connectString == "" {
		return "", "", "", errors.New("ORACLE_DSN contains empty user, password, or connectString")
	}

	return user, password, connectString, nil
}

func escapeDoubleQuotes(value string) string {
	return strings.ReplaceAll(value, `"`, `\"`)
}

func ensureOracleInitialized() error {
	if Oracle == nil {
		return errors.New("oracle connection is not initialized")
	}
	return nil
}

func containsIgnoreCase(value, pattern string) bool {
	return strings.Contains(strings.ToLower(value), strings.ToLower(pattern))
}
