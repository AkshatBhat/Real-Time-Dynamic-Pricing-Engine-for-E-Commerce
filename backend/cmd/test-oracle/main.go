package main

import (
	"dynamic-pricing-engine/backend/internal/config"
	"dynamic-pricing-engine/backend/internal/db"
	"log"
)

func main() {
	cfg, err := config.LoadOracleConfig()
	if err != nil {
		log.Fatalf("Invalid configuration: %v", err)
	}

	if err := db.InitOracle(cfg.OracleDSN, cfg.OracleLibDir, cfg.OracleTimezone); err != nil {
		log.Fatalf("Oracle init failed: %v", err)
	}
}
