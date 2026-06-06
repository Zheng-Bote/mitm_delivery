package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// SaaSAuthConfig defines credentials specific to the generic SaaS adapter.
type SaaSAuthConfig struct {
	APIKey      string `json:"api_key"`
	BearerToken string `json:"bearer_token"`
}

// SaaSAdapter is the DeliverySender implementation for direct SaaS HTTP calls.
type SaaSAdapter struct {
	client *http.Client
}

// NewSaaSAdapter creates a new SaaS adapter with an optional HTTP client (for mocking/tuning).
func NewSaaSAdapter(client *http.Client) *SaaSAdapter {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &SaaSAdapter{client: client}
}

// Send executes the HTTP POST request to the SaaS target.
func (a *SaaSAdapter) Send(ctx context.Context, config TargetConfig, idempotencyKey string, payload []byte) error {
	var authCfg SaaSAuthConfig
	if err := json.Unmarshal(config.AuthConfig, &authCfg); err != nil {
		return &DeliveryError{
			IsTransient:  false,
			ErrorMessage: fmt.Sprintf("invalid auth_config for SaaS adapter: %v", err),
			ErrorCode:    "INVALID_CONFIG",
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, config.EndpointURL, bytes.NewReader(payload))
	if err != nil {
		return &DeliveryError{
			IsTransient:  false,
			ErrorMessage: fmt.Sprintf("failed to create request: %v", err),
			ErrorCode:    "INTERNAL_ERROR",
		}
	}

	// Set standard headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", idempotencyKey)

	// Set Auth
	if authCfg.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+authCfg.BearerToken)
	} else if authCfg.APIKey != "" {
		req.Header.Set("X-API-Key", authCfg.APIKey)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		// Network errors are considered transient (e.g. timeouts, connection refused)
		return &DeliveryError{
			IsTransient:  true,
			ErrorMessage: fmt.Sprintf("network error during request: %v", err),
			ErrorCode:    "NETWORK_ERROR",
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil // Success
	}

	// Read error body for context
	bodyBytes, _ := io.ReadAll(resp.Body)
	errMsg := fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(bodyBytes))

	// Categorize HTTP errors
	isTransient := false
	errorCode := fmt.Sprintf("HTTP_%d", resp.StatusCode)

	if resp.StatusCode == http.StatusTooManyRequests || (resp.StatusCode >= 500 && resp.StatusCode <= 599) {
		isTransient = true
	}

	return &DeliveryError{
		IsTransient:  isTransient,
		HTTPCode:     resp.StatusCode,
		ErrorMessage: errMsg,
		ErrorCode:    errorCode,
	}
}
