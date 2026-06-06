package delivery

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ApigeeAuthConfig defines credentials specific to the APIGEE Gateway adapter.
type ApigeeAuthConfig struct {
	ClientCertPath string `json:"client_cert_path"`
	ClientKeyPath  string `json:"client_key_path"`
	JWTToken       string `json:"jwt_token"`
	RoutingKey     string `json:"routing_key"`
}

// ApigeeAdapter is the DeliverySender implementation for Apigee Gateway calls via mTLS.
type ApigeeAdapter struct {
	client *http.Client
}

// NewApigeeAdapter creates a new APIGEE adapter. If client is nil, it will create one based on the auth config later.
func NewApigeeAdapter(client *http.Client) *ApigeeAdapter {
	return &ApigeeAdapter{client: client}
}

// Send executes the HTTP POST request to the APIGEE target with mTLS and JWT.
func (a *ApigeeAdapter) Send(ctx context.Context, config TargetConfig, idempotencyKey string, payload []byte) error {
	var authCfg ApigeeAuthConfig
	if err := json.Unmarshal(config.AuthConfig, &authCfg); err != nil {
		return &DeliveryError{
			IsTransient:  false,
			ErrorMessage: fmt.Sprintf("invalid auth_config for APIGEE adapter: %v", err),
			ErrorCode:    "INVALID_CONFIG",
		}
	}

	client := a.client
	if client == nil {
		// Initialize mTLS client if paths are provided
		var tlsConfig *tls.Config
		if authCfg.ClientCertPath != "" && authCfg.ClientKeyPath != "" {
			cert, err := tls.LoadX509KeyPair(authCfg.ClientCertPath, authCfg.ClientKeyPath)
			if err != nil {
				return &DeliveryError{
					IsTransient:  false,
					ErrorMessage: fmt.Sprintf("failed to load client certificates: %v", err),
					ErrorCode:    "TLS_ERROR",
				}
			}
			tlsConfig = &tls.Config{Certificates: []tls.Certificate{cert}}
		}

		tr := &http.Transport{TLSClientConfig: tlsConfig}
		client = &http.Client{Transport: tr, Timeout: 15 * time.Second}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, config.EndpointURL, bytes.NewReader(payload))
	if err != nil {
		return &DeliveryError{
			IsTransient:  false,
			ErrorMessage: fmt.Sprintf("failed to create request: %v", err),
			ErrorCode:    "INTERNAL_ERROR",
		}
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", idempotencyKey)

	if authCfg.JWTToken != "" {
		req.Header.Set("Authorization", "Bearer "+authCfg.JWTToken)
	}
	if authCfg.RoutingKey != "" {
		req.Header.Set("X-Apigee-Routing", authCfg.RoutingKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		return &DeliveryError{
			IsTransient:  true,
			ErrorMessage: fmt.Sprintf("network error during apigee request: %v", err),
			ErrorCode:    "NETWORK_ERROR",
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	bodyBytes, _ := io.ReadAll(resp.Body)
	errMsg := fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(bodyBytes))

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
