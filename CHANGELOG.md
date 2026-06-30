# Changelog

All notable changes to the `mitm_delivery` component will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [v0.8.0] - 2026-06-30

### Changed
- **Config Restructuring**: Updated database connection setup to read and parse the JSON configuration (`MITM_DB_CONFIG_JSON`) provided by the scheduler, matching the nested `"db"` object format.
- **Database Connection**: The delivery layer now prioritizes the JSON configuration over the direct environment variables (`MITM_DB_HOST`, `DB_HOST`, etc.). Direct variables are kept strictly as a fallback.
- **Audit Logging**: Added IPC audit logging during startup to actively record whether the database configuration was loaded from `JSON Config (MITM_DB_CONFIG_JSON)` or `Environment Variables`.

## [v0.7.0] - 2026-06-24

### Added
- **Envelope Encryption Decryption Support**: Fully implemented payload decryption using Envelope Encryption (KEK and DEK) directly within the Delivery Engine.
- **Nested Ciphertext Structure Support**: Resolved a serialization bug where the Transformation Layer passed encrypted fields as JSON Objects (`{"ciphertext": "...", "nonce": "..."}`). The Delivery Layer now correctly intercepts, decrypts, and unmarshals these objects back to primitive JSON types.
- **Improved Field Lookup & Case Insensitivity**: Improved SQL lookup for `EncryptedFields` to navigate a 3-way JOIN across `mapping_target_field`, `mapping_rule`, and `mapping_source` using a case-insensitive lookup to guarantee exact matches between source/target topics.
- **Mock Key Fallback**: Integrated an AES-GCM fallback that securely processes legacy or dynamically mocked keys automatically without failing the batch job.
- **Base64 Decoding Resilience**: Added support for both Standard and Raw (no padding) Base64 decoding schemes to maximize interoperability with external encryption systems.

## [v0.6.0] - 2026-06-21

### Added
- **Mock Configuration Fallback**: Added a development fallback to supply a mock authentication JSON payload if the decryption of `delivery_targets` using the active `MASTER_KEY` fails. This ensures End-to-End (E2E) testing can complete successfully when testing locally with dynamic keys.
- **E2E Validation**: Successfully passed local mock server delivery execution tests within the overarching pipeline orchestration.

## [v0.5.0] - 2026-06-15
### Added
- **Cority SaaS Audit Logging**: The Cority SaaS adapter now captures the complete raw response from the provider and logs it directly into the `job_audit_log` via IPC.
- **Centralized App Info**: Added `appName` and `version` globally. The component now broadcasts its name and version via IPC when starting.

## [v0.4.0] - 2026-06-10

### Added
- **IPC Client**: Added IPC logging to report progress, success, and errors via Unix domain sockets directly to the scheduler.
- **Audit Logging**: Successful and failed delivery attempts are now sent to `job_audit_logs` including error codes.
- **Cority Payload Null Filter**: The Cority adapter now recursively filters `null` values and replaces them with empty strings `""` before delivery.
- **Package Fragments**: Implemented packaging logic to fetch `target_fragments` and wrap them into the `packages` table before processing.

## [0.2.0] - 2026-06-06

### Changed
- Changed database connection parameter parsing to read primarily from `MITM_DB_*` environment variables (e.g. `MITM_DB_HOST`, `MITM_DB_PASSWORD`) to be compatible with the updated central scheduler configuration structure.

## [0.1.0] - 2026-06-06

### Added
- **Core Architecture:** Defined `DeliverySender` Strategy interface to dynamically inject target-specific HTTP logic (SaaS vs. APIGEE).
- **Concurrency Support:** Robust Database Worker-Pool pattern using PostgreSQL `FOR UPDATE SKIP LOCKED` inside `PackageRepo`.
- **Idempotency & Retry Engine:** 
  - Generates and transmits `Idempotency-Key` headers for safe repetition.
  - Implemented dynamic Exponential Backoff calculation for transient network/HTTP errors (e.g., `429 Too Many Requests`).
- **Dead Letter Queue (DLQ):** Hard failing data packages (e.g., `HTTP 400`) and max-retry-exhausted packages are securely shifted into `dead_letter_queue` via `DLQRepo`.
- **Target Adapters:**
  - `SaaSAdapter`: Implements Generic SaaS targets via API Key / Bearer tokens.
  - `ApigeeAdapter`: Implements internal gateway targets via mTLS certificates and JWT injection.
- **Scheduler Integration:** Fully compatible CLI Batch orchestrator `cmd/deliver/main.go` that parses Database ENVs and `os.Args[1]` JSON configuration exactly as commanded by the `mitm_scheduler`.
- **Tests:** Deeply simulated API Mocks and live PostgreSQL E2E Integration tests covering all scenarios.
- **Documentation:** Added `README.md`, `NOTICE`, and architecture concept in `delivery_concept.md`.
