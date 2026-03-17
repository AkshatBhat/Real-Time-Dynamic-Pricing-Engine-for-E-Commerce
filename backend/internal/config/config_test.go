package config

import "testing"

func TestLoadEventProducerConfigDefaults(t *testing.T) {
	t.Setenv(envKafkaBrokers, "")
	t.Setenv(envProductEventsTopic, "")

	cfg, err := LoadEventProducerConfig()
	if err != nil {
		t.Fatalf("expected default event-producer config, got error: %v", err)
	}

	if len(cfg.KafkaBrokers) != 1 || cfg.KafkaBrokers[0] != defaultKafkaBrokers {
		t.Fatalf("unexpected kafka brokers: %#v", cfg.KafkaBrokers)
	}
	if cfg.ProductEventsTopic != defaultProductEvents {
		t.Fatalf("unexpected product events topic: %q", cfg.ProductEventsTopic)
	}
}

func TestLoadPricingEngineConfigEnvOverride(t *testing.T) {
	t.Setenv(envKafkaBrokers, "kafka-1:9092,kafka-2:9093")
	t.Setenv(envRedisAddr, "redis-host:6380")
	t.Setenv(envProductEventsTopic, "input-events")
	t.Setenv(envPriceUpdatesTopic, "output-prices")
	t.Setenv(envOracleDSN, "pricing/pricingpass@oracle-host:1521/FREEPDB1")
	t.Setenv(envOracleLibDir, "/opt/oracle/client")
	t.Setenv(envOracleTimezone, "America/Chicago")

	cfg, err := LoadPricingEngineConfig()
	if err != nil {
		t.Fatalf("expected overridden pricing-engine config, got error: %v", err)
	}

	if got, want := len(cfg.KafkaBrokers), 2; got != want {
		t.Fatalf("unexpected brokers count: got %d, want %d", got, want)
	}
	if cfg.RedisAddr != "redis-host:6380" {
		t.Fatalf("unexpected redis addr: %q", cfg.RedisAddr)
	}
	if cfg.ProductEventsTopic != "input-events" {
		t.Fatalf("unexpected product events topic: %q", cfg.ProductEventsTopic)
	}
	if cfg.PriceUpdatesTopic != "output-prices" {
		t.Fatalf("unexpected price updates topic: %q", cfg.PriceUpdatesTopic)
	}
	if cfg.OracleDSN != "pricing/pricingpass@oracle-host:1521/FREEPDB1" {
		t.Fatalf("unexpected oracle dsn: %q", cfg.OracleDSN)
	}
	if cfg.OracleLibDir != "/opt/oracle/client" {
		t.Fatalf("unexpected oracle lib dir: %q", cfg.OracleLibDir)
	}
	if cfg.OracleTimezone != "America/Chicago" {
		t.Fatalf("unexpected oracle timezone: %q", cfg.OracleTimezone)
	}
}

func TestLoadAPIConfigIncludesOracleSettings(t *testing.T) {
	t.Setenv(envKafkaBrokers, "kafka-1:9092,kafka-2:9093")
	t.Setenv(envPriceUpdatesTopic, "output-prices")
	t.Setenv(envRedisAddr, "redis-host:6380")
	t.Setenv(envAPIPort, "9090")
	t.Setenv(envAdminAPIKey, "phase4-secret")
	t.Setenv(envAPIKeyHeader, "X-Admin-Key")
	t.Setenv(envOracleDSN, "pricing/pricingpass@oracle-host:1521/FREEPDB1")
	t.Setenv(envOracleLibDir, "/opt/oracle/client")
	t.Setenv(envOracleTimezone, "UTC")

	cfg, err := LoadAPIConfig()
	if err != nil {
		t.Fatalf("expected valid API config, got error: %v", err)
	}

	if cfg.RedisAddr != "redis-host:6380" {
		t.Fatalf("unexpected redis addr: %q", cfg.RedisAddr)
	}
	if got, want := len(cfg.KafkaBrokers), 2; got != want {
		t.Fatalf("unexpected brokers count: got %d, want %d", got, want)
	}
	if cfg.PriceUpdatesTopic != "output-prices" {
		t.Fatalf("unexpected price updates topic: %q", cfg.PriceUpdatesTopic)
	}
	if cfg.Port != 9090 {
		t.Fatalf("unexpected API port: %d", cfg.Port)
	}
	if cfg.AdminAPIKey != "phase4-secret" {
		t.Fatalf("unexpected admin api key: %q", cfg.AdminAPIKey)
	}
	if cfg.APIKeyHeader != "X-Admin-Key" {
		t.Fatalf("unexpected api key header: %q", cfg.APIKeyHeader)
	}
	if cfg.OracleDSN != "pricing/pricingpass@oracle-host:1521/FREEPDB1" {
		t.Fatalf("unexpected oracle dsn: %q", cfg.OracleDSN)
	}
	if cfg.OracleLibDir != "/opt/oracle/client" {
		t.Fatalf("unexpected oracle lib dir: %q", cfg.OracleLibDir)
	}
	if cfg.OracleTimezone != "UTC" {
		t.Fatalf("unexpected oracle timezone: %q", cfg.OracleTimezone)
	}
}

func TestLoadConfigInvalidValues(t *testing.T) {
	t.Setenv(envKafkaBrokers, "invalid-broker")
	if _, err := LoadEventProducerConfig(); err == nil {
		t.Fatalf("expected invalid broker format to fail")
	}

	t.Setenv(envKafkaBrokers, "")
	t.Setenv(envRedisAddr, "bad-redis-address")
	if _, err := LoadPricingEngineConfig(); err == nil {
		t.Fatalf("expected invalid redis address to fail")
	}

	t.Setenv(envRedisAddr, "")
	t.Setenv(envKafkaBrokers, "")
	t.Setenv(envPriceUpdatesTopic, "")
	t.Setenv(envAPIPort, "not-a-port")
	t.Setenv(envAdminAPIKey, "phase4-secret")
	if _, err := LoadAPIConfig(); err == nil {
		t.Fatalf("expected invalid API_PORT to fail")
	}

	t.Setenv(envKafkaBrokers, "")
	t.Setenv(envPriceUpdatesTopic, "bad topic name")
	t.Setenv(envAPIPort, "8080")
	if _, err := LoadAPIConfig(); err == nil {
		t.Fatalf("expected invalid PRICE_UPDATES_TOPIC to fail")
	}

	t.Setenv(envKafkaBrokers, "invalid-broker")
	t.Setenv(envPriceUpdatesTopic, "")
	if _, err := LoadAPIConfig(); err == nil {
		t.Fatalf("expected invalid KAFKA_BROKERS to fail for API config")
	}

	t.Setenv(envAPIPort, "")
	t.Setenv(envKafkaBrokers, "")
	t.Setenv(envPriceUpdatesTopic, "")
	t.Setenv(envProductEventsTopic, "bad topic name")
	if _, err := LoadEventProducerConfig(); err == nil {
		t.Fatalf("expected topic with whitespace to fail")
	}

	t.Setenv(envProductEventsTopic, "")
	t.Setenv(envAPIPort, "8080")
	t.Setenv(envAPIKeyHeader, "invalid key")
	if _, err := LoadAPIConfig(); err == nil {
		t.Fatalf("expected invalid API_KEY_HEADER to fail")
	}
}

func TestLoadAPIConfigRequiresAdminAPIKey(t *testing.T) {
	t.Setenv(envRedisAddr, "")
	t.Setenv(envAPIPort, "")
	t.Setenv(envAdminAPIKey, "")
	t.Setenv(envAPIKeyHeader, "")

	if _, err := LoadAPIConfig(); err == nil {
		t.Fatalf("expected missing ADMIN_API_KEY to fail")
	}
}

func TestLoadAPIConfigAPIKeyHeaderDefault(t *testing.T) {
	t.Setenv(envRedisAddr, "")
	t.Setenv(envAPIPort, "")
	t.Setenv(envAdminAPIKey, "phase4-secret")
	t.Setenv(envAPIKeyHeader, "")

	cfg, err := LoadAPIConfig()
	if err != nil {
		t.Fatalf("expected API config to load with default API key header, got error: %v", err)
	}
	if cfg.APIKeyHeader != defaultAPIKeyHeader {
		t.Fatalf("unexpected default API key header: %q", cfg.APIKeyHeader)
	}
}
