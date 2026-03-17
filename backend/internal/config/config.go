package config

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
)

const (
	defaultKafkaBrokers   = "localhost:9092"
	defaultRedisAddr      = "localhost:6379"
	defaultProductEvents  = "product-events"
	defaultPriceUpdates   = "price-updates"
	defaultAPIPort        = "8080"
	defaultAPIKeyHeader   = "X-API-Key"
	defaultOracleDSN      = "pricing/pricingpass@localhost:1521/FREEPDB1"
	defaultOracleTimezone = "UTC"
	envKafkaBrokers       = "KAFKA_BROKERS"
	envRedisAddr          = "REDIS_ADDR"
	envProductEventsTopic = "PRODUCT_EVENTS_TOPIC"
	envPriceUpdatesTopic  = "PRICE_UPDATES_TOPIC"
	envAPIPort            = "API_PORT"
	envAdminAPIKey        = "ADMIN_API_KEY"
	envAPIKeyHeader       = "API_KEY_HEADER"
	envOracleDSN          = "ORACLE_DSN"
	envOracleLibDir       = "ORACLE_LIB_DIR"
	envOracleTimezone     = "ORACLE_TIMEZONE"
)

var dotenvLoadOnce sync.Once
var dotenvLoadErr error

type EventProducerConfig struct {
	KafkaBrokers       []string
	ProductEventsTopic string
}

type PricingEngineConfig struct {
	KafkaBrokers       []string
	RedisAddr          string
	ProductEventsTopic string
	PriceUpdatesTopic  string
	OracleDSN          string
	OracleLibDir       string
	OracleTimezone     string
}

type APIConfig struct {
	KafkaBrokers      []string
	PriceUpdatesTopic string
	RedisAddr         string
	Port              int
	AdminAPIKey       string
	APIKeyHeader      string
	OracleDSN         string
	OracleLibDir      string
	OracleTimezone    string
}

type OracleConfig struct {
	OracleDSN      string
	OracleLibDir   string
	OracleTimezone string
}

func LoadEventProducerConfig() (EventProducerConfig, error) {
	if err := loadDotEnvIfPresent(); err != nil {
		return EventProducerConfig{}, err
	}

	brokers, err := parseKafkaBrokers(envOrDefault(envKafkaBrokers, defaultKafkaBrokers))
	if err != nil {
		return EventProducerConfig{}, err
	}

	topic, err := validateTopic(envProductEventsTopic, envOrDefault(envProductEventsTopic, defaultProductEvents))
	if err != nil {
		return EventProducerConfig{}, err
	}

	return EventProducerConfig{
		KafkaBrokers:       brokers,
		ProductEventsTopic: topic,
	}, nil
}

func LoadPricingEngineConfig() (PricingEngineConfig, error) {
	if err := loadDotEnvIfPresent(); err != nil {
		return PricingEngineConfig{}, err
	}

	brokers, err := parseKafkaBrokers(envOrDefault(envKafkaBrokers, defaultKafkaBrokers))
	if err != nil {
		return PricingEngineConfig{}, err
	}

	redisAddr, err := validateHostPort(envRedisAddr, envOrDefault(envRedisAddr, defaultRedisAddr))
	if err != nil {
		return PricingEngineConfig{}, err
	}

	productEventsTopic, err := validateTopic(envProductEventsTopic, envOrDefault(envProductEventsTopic, defaultProductEvents))
	if err != nil {
		return PricingEngineConfig{}, err
	}

	priceUpdatesTopic, err := validateTopic(envPriceUpdatesTopic, envOrDefault(envPriceUpdatesTopic, defaultPriceUpdates))
	if err != nil {
		return PricingEngineConfig{}, err
	}

	oracleDSN := envOrDefault(envOracleDSN, defaultOracleDSN)
	if oracleDSN == "" {
		return PricingEngineConfig{}, fmt.Errorf("%s cannot be empty", envOracleDSN)
	}

	return PricingEngineConfig{
		KafkaBrokers:       brokers,
		RedisAddr:          redisAddr,
		ProductEventsTopic: productEventsTopic,
		PriceUpdatesTopic:  priceUpdatesTopic,
		OracleDSN:          oracleDSN,
		OracleLibDir:       strings.TrimSpace(os.Getenv(envOracleLibDir)),
		OracleTimezone:     envOrDefault(envOracleTimezone, defaultOracleTimezone),
	}, nil
}

func LoadAPIConfig() (APIConfig, error) {
	if err := loadDotEnvIfPresent(); err != nil {
		return APIConfig{}, err
	}

	brokers, err := parseKafkaBrokers(envOrDefault(envKafkaBrokers, defaultKafkaBrokers))
	if err != nil {
		return APIConfig{}, err
	}

	priceUpdatesTopic, err := validateTopic(envPriceUpdatesTopic, envOrDefault(envPriceUpdatesTopic, defaultPriceUpdates))
	if err != nil {
		return APIConfig{}, err
	}

	redisAddr, err := validateHostPort(envRedisAddr, envOrDefault(envRedisAddr, defaultRedisAddr))
	if err != nil {
		return APIConfig{}, err
	}

	port, err := parsePort(envOrDefault(envAPIPort, defaultAPIPort))
	if err != nil {
		return APIConfig{}, err
	}

	oracleDSN := envOrDefault(envOracleDSN, defaultOracleDSN)
	if oracleDSN == "" {
		return APIConfig{}, fmt.Errorf("%s cannot be empty", envOracleDSN)
	}

	adminAPIKey := strings.TrimSpace(os.Getenv(envAdminAPIKey))
	if adminAPIKey == "" {
		return APIConfig{}, fmt.Errorf("%s cannot be empty", envAdminAPIKey)
	}

	apiKeyHeader, err := validateHeaderName(envAPIKeyHeader, envOrDefault(envAPIKeyHeader, defaultAPIKeyHeader))
	if err != nil {
		return APIConfig{}, err
	}

	return APIConfig{
		KafkaBrokers:      brokers,
		PriceUpdatesTopic: priceUpdatesTopic,
		RedisAddr:         redisAddr,
		Port:              port,
		AdminAPIKey:       adminAPIKey,
		APIKeyHeader:      apiKeyHeader,
		OracleDSN:         oracleDSN,
		OracleLibDir:      strings.TrimSpace(os.Getenv(envOracleLibDir)),
		OracleTimezone:    envOrDefault(envOracleTimezone, defaultOracleTimezone),
	}, nil
}

func LoadOracleConfig() (OracleConfig, error) {
	if err := loadDotEnvIfPresent(); err != nil {
		return OracleConfig{}, err
	}

	oracleDSN := envOrDefault(envOracleDSN, defaultOracleDSN)
	if oracleDSN == "" {
		return OracleConfig{}, fmt.Errorf("%s cannot be empty", envOracleDSN)
	}

	return OracleConfig{
		OracleDSN:      oracleDSN,
		OracleLibDir:   strings.TrimSpace(os.Getenv(envOracleLibDir)),
		OracleTimezone: envOrDefault(envOracleTimezone, defaultOracleTimezone),
	}, nil
}

func envOrDefault(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func parseKafkaBrokers(raw string) ([]string, error) {
	parts := strings.Split(raw, ",")
	brokers := make([]string, 0, len(parts))
	for _, part := range parts {
		broker, err := validateHostPort(envKafkaBrokers, part)
		if err != nil {
			return nil, err
		}
		brokers = append(brokers, broker)
	}
	if len(brokers) == 0 {
		return nil, fmt.Errorf("%s must contain at least one broker", envKafkaBrokers)
	}
	return brokers, nil
}

func validateHostPort(name, raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", fmt.Errorf("%s cannot be empty", name)
	}

	host, port, err := net.SplitHostPort(value)
	if err != nil {
		return "", fmt.Errorf("%s must be host:port, got %q", name, value)
	}
	if strings.TrimSpace(host) == "" {
		return "", fmt.Errorf("%s host cannot be empty", name)
	}

	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return "", fmt.Errorf("%s port must be a number between 1 and 65535, got %q", name, port)
	}

	return value, nil
}

func validateTopic(name, raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", fmt.Errorf("%s cannot be empty", name)
	}
	if strings.ContainsAny(value, " \t\n\r") {
		return "", fmt.Errorf("%s cannot contain whitespace, got %q", name, value)
	}
	return value, nil
}

func validateHeaderName(name, raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", fmt.Errorf("%s cannot be empty", name)
	}
	for _, ch := range value {
		if (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-' {
			continue
		}
		return "", fmt.Errorf("%s must contain only letters, numbers, and hyphens, got %q", name, value)
	}
	return value, nil
}

func parsePort(raw string) (int, error) {
	value := strings.TrimSpace(raw)
	port, err := strconv.Atoi(value)
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("%s must be a number between 1 and 65535, got %q", envAPIPort, raw)
	}
	return port, nil
}

func loadDotEnvIfPresent() error {
	dotenvLoadOnce.Do(func() {
		content, err := os.ReadFile(".env")
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return
			}
			dotenvLoadErr = fmt.Errorf("failed reading .env: %w", err)
			return
		}

		scanner := bufio.NewScanner(strings.NewReader(string(content)))
		lineNo := 0
		for scanner.Scan() {
			lineNo++
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}

			if strings.HasPrefix(line, "export ") {
				line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
			}

			parts := strings.SplitN(line, "=", 2)
			if len(parts) != 2 {
				dotenvLoadErr = fmt.Errorf(".env parse error on line %d: expected KEY=VALUE", lineNo)
				return
			}

			key := strings.TrimSpace(parts[0])
			if key == "" {
				dotenvLoadErr = fmt.Errorf(".env parse error on line %d: empty key", lineNo)
				return
			}

			value := strings.TrimSpace(parts[1])
			value = strings.Trim(value, `"'`)

			if _, exists := os.LookupEnv(key); exists {
				continue
			}
			if err := os.Setenv(key, value); err != nil {
				dotenvLoadErr = fmt.Errorf("failed setting %s from .env: %w", key, err)
				return
			}
		}

		if err := scanner.Err(); err != nil {
			dotenvLoadErr = fmt.Errorf("failed scanning .env: %w", err)
		}
	})

	return dotenvLoadErr
}
