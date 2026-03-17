# Real-Time Dynamic Pricing Engine for E-Commerce — Product Requirements Document

## 1. Project Title

**Real-Time Dynamic Pricing Engine for E-Commerce**

## 2. Overview

Build a full-stack, event-driven e-commerce pricing platform that updates product prices in near real time based on user activity signals such as views, cart additions, and purchases. The system should expose live product pricing through an API, persist price history, and support future admin price overrides.

This project is intended as a strong MVP for resume and demo purposes, with local-first development and later AWS deployment.

## 3. Goals

* Build a realistic event-driven backend using Kafka and Go
* Compute dynamic prices from streaming demand signals
* Serve latest price and demand through a REST API
* Persist price history in Oracle DB
* Support later extension for admin overrides and frontend dashboards
* Keep architecture production-inspired but MVP-feasible

## 4. Non-Goals

* Full authentication/authorization
* Full checkout/catalog/order management
* Complex ML-based pricing
* Production-grade observability/security at MVP stage
* Multi-region or highly available cloud deployment in current phase

## 5. Users

* **Admin / Pricing Analyst**: wants to monitor dynamic pricing behavior and later set manual overrides
* **Frontend / Client App**: wants to fetch live product price and demand
* **Developer / Recruiter Demo Viewer**: wants to understand streaming, caching, persistence, and API flow

## 6. Core Use Cases

1. Simulate product activity events in real time
2. Consume those events and update product demand
3. Recalculate product price dynamically
4. Store live demand and live price in Redis
5. Publish price updates to Kafka
6. Persist price history in Oracle DB
7. Fetch current product price and demand via REST API
8. Later: allow admin override price through API and UI

## 7. Functional Requirements

### 7.1 Event Producer

* Generate fake product events continuously
* Event types: `view`, `cart`, `purchase`
* Publish events to Kafka topic `product-events`

### 7.2 Pricing Engine

* Consume events from Kafka topic `product-events`
* Maintain demand score per product in Redis
* Use weighted demand scoring:

  * `view = 1`
  * `cart = 3`
  * `purchase = 5`
* Compute updated price using current demand and event price
* Store latest computed price in Redis
* Publish computed price to Kafka topic `price-updates`
* Insert each computed price into Oracle `price_history`

### 7.3 API Server

* Expose endpoint:

  * `GET /price/{productID}`
* Response should include:

  * `product_id`
  * `price`
  * `demand`
* Read current price and demand from Redis
* Later: if Oracle override exists, return override price instead of Redis price

### 7.4 Database

* Oracle stores persistent records:

  * `price_history`
  * `price_overrides`
* Redis stores fast-changing real-time state:

  * current demand per product
  * current live price per product

### 7.5 Frontend (Later Phase)

* Next.js + Tailwind CSS dashboard
* Product pricing view
* Admin override form
* Pricing history visualization
* Demand and price monitoring widgets

## 8. Non-Functional Requirements

* Local development via Docker Compose
* Modular Go code organized by service
* Near real-time event processing
* Easy to demo from terminal + API + SQL Developer
* Cloud deployable later on AWS using Terraform, Kubernetes, Jenkins

## 9. Pricing Logic (Current MVP)

For each incoming event:

* Increment Redis demand score using weighted event type
* Compute new price from incoming event price and demand score

Current formula:

```text
new_price = event_price + sqrt(demand) * 5
```

This is a simple heuristic for MVP and can later be replaced with inventory-aware or margin-aware pricing logic.

## 10. Data Model

### Kafka Topics

* `product-events`
* `price-updates`

### Redis Keys

* `product:demand:{productId}`
* `product:price:{productId}`

### Oracle Tables

* `price_history(id, product_id, price, event_time)`
* `price_overrides(id, product_id, override_price, reason, set_time)`

## 11. Tech Stack

### Backend

* **Golang**
* **Apache Kafka**
* **Go Chi** for REST API
* **Sarama / IBM Sarama** for Kafka client

### Databases / Caching

* **Redis**
* **Oracle Database Free 23c** in Docker
* **godror** Go Oracle driver

### DevOps / Local Infra

* **Docker Desktop**
* **Docker Compose**
* Later: **Kubernetes**, **Jenkins**, **Terraform**

### Frontend

* **Next.js**
* **Tailwind CSS**

### Cloud (Later)

* **AWS**

  * EKS
  * MSK
  * ElastiCache
  * RDS/Oracle alternative decision later
  * Terraform-managed infra

## 12. Local Architecture

1. Event Producer sends fake e-commerce events to Kafka
2. Pricing Engine consumes events from Kafka
3. Pricing Engine updates Redis demand and price
4. Pricing Engine publishes price updates to Kafka
5. Pricing Engine writes price history to Oracle
6. API Server serves latest price and demand from Redis
7. SQL Developer can inspect Oracle history

## 13. Success Criteria

* Product events are continuously generated and consumed
* Redis demand and price keys update correctly
* API returns correct live price and demand
* Oracle stores price history rows successfully
* SQL Developer can inspect persisted history
* System works fully locally on macOS Apple Silicon

## 14. Resume Positioning

### 14.1 Current Resume Version

**Real-Time Dynamic Pricing Engine for E-Commerce | C++, Apache Kafka, Redis, Oracle DB, AWS EKS, Terraform**

* Processed over 500K product events/day with sub-second pricing updates using Kafka + Redis pipeline
* Launched dynamic pricing engine on AWS EKS via Terraform, scaling backend for 1K+ concurrent users with <200ms latency

### 14.2 Minimum Progress Needed to Fairly Support Resume Claims

At minimum, the project should include:

* working event generation through Kafka
* real-time price computation using demand signals
* Redis-backed live demand and price storage
* REST API exposing current product price and demand
* Oracle-backed persistence of price history
* local end-to-end demo showing streaming updates
* later cloud deployment if AWS/EKS/Terraform lines are to remain on the resume

### 14.3 Recommended Final Resume Version

**Real-Time Dynamic Pricing Engine for E-Commerce | Go, Apache Kafka, Redis, Oracle DB, Next.js, Tailwind CSS, Docker, Kubernetes, Terraform, AWS**

* Processed over 500K simulated product events/day through a Go + Kafka pipeline, computing demand-weighted prices in real time and serving live price updates from Redis-backed APIs
* Persisted dynamic price history in Oracle DB and built a full-stack monitoring workflow with REST endpoints and a Next.js dashboard for price and demand visibility
* Containerized and deployed the platform with Docker, Kubernetes, Terraform, and AWS infrastructure, supporting 1K+ concurrent price lookups with sub-200ms API latency

## 15. Future Enhancements

* Manual override API + UI
* Frontend dashboard
* Inventory-aware pricing
* Competitor price ingestion
* Historical analytics
* Rate limiting, auth, admin roles
* Jenkins pipeline
* Kubernetes deployment
* Terraform AWS infrastructure
