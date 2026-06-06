# MitM Delivery Layer (CLI Batch Job)

This directory contains the Go implementation of the MitM Delivery Layer. It serves as the final stage of the Data Aggregator pipeline, reliably pushing transformed JSON packages from PostgreSQL into external target systems.

## Overview

The Delivery Layer is designed for extreme resilience and high concurrency:
- **Idempotency:** Every delivery attempt is bound to an `Idempotency-Key` (UUID) to prevent duplicated records if network timeouts occur.
- **Retry Engine:** Implements Exponential Backoff for transient errors (e.g., `HTTP 429 Too Many Requests` or `HTTP 503 Service Unavailable`).
- **Dead Letter Queue (DLQ):** Permanently failing deliveries (e.g., `HTTP 400 Bad Request` or exhausting retry limits) are safely moved into the `dead_letter_queue` table to prevent queue blocking.
- **Interchangeable Adapters:** Extensible architecture using Strategy Pattern to support multiple target platforms.

## Supported Adapters

1. **SaaS Adapter (`SAAS`)**: Direct HTTP interactions utilizing standard Bearer Tokens or API keys.
2. **APIGEE Adapter (`APIGEE`)**: Enterprise gateway support featuring Mutual TLS (mTLS), JWT injection, and specific routing headers.

## Building

To build the executable, run:
```bash
go build -o bin/mitm-deliver ./cmd/deliver/main.go
```

## Usage

The `mitm-deliver` module is executed as a short-lived batch job by the `mitm_scheduler`. It expects:
1. **Environment Variables** for PostgreSQL connections.
2. **A single JSON Argument (`os.Args[1]`)** for job configuration and target endpoint routing.

### Example Execution

```bash
# 1. Provide Database connection via ENV
export MITM_DB_HOST="192.168.0.31"
export MITM_DB_PORT="5432"
export MITM_DB_USER="mitm_user"
export MITM_DB_PASSWORD="secret"
export MITM_DB_NAME="mitm"

# 2. Define the Target and Job parameters
ARGS_JSON='{
  "workers": 5,
  "batch_size": 200,
  "max_retries": 5,
  "target_config": {
    "adapter_type": "SAAS",
    "endpoint_url": "https://api.saas-vendor.com/v1/ingest",
    "auth_config": {
      "api_key": "YOUR_API_KEY"
    }
  }
}'

./bin/mitm-deliver "$ARGS_JSON"
```

### Advanced Config: APIGEE mTLS
If you route via APIGEE, adjust the `target_config`:
```json
"target_config": {
  "adapter_type": "APIGEE",
  "endpoint_url": "https://gateway.internal.corp/mitm",
  "auth_config": {
    "client_cert_path": "/opt/certs/client.crt",
    "client_key_path": "/opt/certs/client.key",
    "jwt_token": "...",
    "routing_key": "vendor-x"
  }
}
```

## Database Interaction
The job interacts strictly with the tables predefined in `./migrations`:
- `packages`: Pulls pending/retryable records.
- `dead_letter_queue`: Safely unloads fatal records. 

Concurrency is handled natively in Postgres using `FOR UPDATE SKIP LOCKED`.
