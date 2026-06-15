package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type CorityAuthConfig struct {
	LoginUser       string `json:"login_user"`
	LoginPass       string `json:"login_pass"`
	AuthRefreshPath string `json:"auth_refresh_path"`
	AuthTokenPath   string `json:"auth_token_path"`
	ImportPath      string `json:"import_path"`
	UploadOptions   struct {
		ImportMode                  string `json:"import_mode"`
		BatchSize                   int    `json:"batch_size"`
		UpdateExistingRecords       bool   `json:"updateExistingRecords"`
		InsertBaseTables            bool   `json:"insertBaseTables"`
		ForceLookupTableUpdate      bool   `json:"forceLookupTableUpdate"`
		DisableSegUpdate            bool   `json:"disableSegUpdate"`
		AutoCreatePortalUser        bool   `json:"autoCreatePortalUser"`
		MergeRecordsWithMatchingSsn bool   `json:"mergeRecordsWithMatchingSsn"`
		DateFormat                  string `json:"dateFormat"`
	} `json:"upload_options"`
}

type CorityAdapter struct {
	client   *http.Client
	logAudit func(string)
}

func NewCorityAdapter(client *http.Client, logAudit func(string)) *CorityAdapter {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &CorityAdapter{client: client, logAudit: logAudit}
}

func (a *CorityAdapter) Send(ctx context.Context, config TargetConfig, idempotencyKey string, payload []byte) error {
	var cfg CorityAuthConfig
	if err := json.Unmarshal(config.AuthConfig, &cfg); err != nil {
		return &DeliveryError{
			IsTransient:  false,
			ErrorMessage: fmt.Sprintf("invalid auth_config for Cority adapter: %v", err),
			ErrorCode:    "INVALID_CONFIG",
		}
	}

	baseURL := strings.TrimSuffix(config.EndpointURL, "/")

	// 1. Get Refresh Token
	refreshReqURL := baseURL + cfg.AuthRefreshPath
	refreshBody := fmt.Sprintf(`{"username":"%s", "password":"%s"}`, cfg.LoginUser, cfg.LoginPass)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, refreshReqURL, strings.NewReader(refreshBody))
	req.Header.Set("Content-Type", "application/json")
	
	resp, err := a.client.Do(req)
	if err != nil {
		return &DeliveryError{IsTransient: true, ErrorMessage: fmt.Sprintf("failed refresh request: %v", err), ErrorCode: "NETWORK_ERROR"}
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return &DeliveryError{IsTransient: false, ErrorMessage: fmt.Sprintf("refresh token failed: %s", string(bodyBytes)), ErrorCode: "AUTH_FAIL"}
	}
	
	var refreshResp struct {
		RefreshToken string `json:"refreshToken"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&refreshResp); err != nil {
		return &DeliveryError{IsTransient: false, ErrorMessage: "failed to parse refresh token", ErrorCode: "AUTH_FAIL"}
	}

	// 2. Get Access Token
	tokenReqURL := baseURL + cfg.AuthTokenPath
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshResp.RefreshToken)

	req2, _ := http.NewRequestWithContext(ctx, http.MethodPost, tokenReqURL, strings.NewReader(form.Encode()))
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	
	resp2, err := a.client.Do(req2)
	if err != nil {
		return &DeliveryError{IsTransient: true, ErrorMessage: fmt.Sprintf("failed token request: %v", err), ErrorCode: "NETWORK_ERROR"}
	}
	defer resp2.Body.Close()
	
	if resp2.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp2.Body)
		return &DeliveryError{IsTransient: false, ErrorMessage: fmt.Sprintf("access token failed: %s", string(bodyBytes)), ErrorCode: "AUTH_FAIL"}
	}
	
	var tokenResp struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&tokenResp); err != nil {
		return &DeliveryError{IsTransient: false, ErrorMessage: "failed to parse access token", ErrorCode: "AUTH_FAIL"}
	}

	// 3. Send Payload Data
	var rawData []interface{}
	if err := json.Unmarshal(payload, &rawData); err != nil {
		return &DeliveryError{IsTransient: false, ErrorMessage: "payload is not valid JSON array", ErrorCode: "INVALID_PAYLOAD"}
	}
	
	// Convert nulls to empty strings recursively
	var replaceNulls func(interface{}) interface{}
	replaceNulls = func(data interface{}) interface{} {
		switch v := data.(type) {
		case map[string]interface{}:
			for key, val := range v {
				if val == nil {
					v[key] = ""
				} else {
					v[key] = replaceNulls(val)
				}
			}
			return v
		case []interface{}:
			for i, val := range v {
				if val == nil {
					v[i] = ""
				} else {
					v[i] = replaceNulls(val)
				}
			}
			return v
		default:
			return data
		}
	}
	
	for i, item := range rawData {
		rawData[i] = replaceNulls(item)
	}
	
	finalBody := map[string]interface{}{
		"options": cfg.UploadOptions,
		"data": rawData,
	}
	
	finalBytes, _ := json.Marshal(finalBody)
	
	importReqURL := baseURL + cfg.ImportPath
	req3, _ := http.NewRequestWithContext(ctx, http.MethodPost, importReqURL, bytes.NewReader(finalBytes))
	req3.Header.Set("Content-Type", "application/json")
	req3.Header.Set("Authorization", "Bearer "+tokenResp.AccessToken)
	req3.Header.Set("Idempotency-Key", idempotencyKey)

	resp3, err := a.client.Do(req3)
	if err != nil {
		return &DeliveryError{IsTransient: true, ErrorMessage: fmt.Sprintf("network error during upload: %v", err), ErrorCode: "NETWORK_ERROR"}
	}
	defer resp3.Body.Close()

	bodyBytes, _ := io.ReadAll(resp3.Body)
	
	if a.logAudit != nil && len(bodyBytes) > 0 {
		a.logAudit(string(bodyBytes))
	}

	if resp3.StatusCode >= 200 && resp3.StatusCode < 300 {
		return nil
	}

	isTransient := false
	if resp3.StatusCode == http.StatusTooManyRequests || (resp3.StatusCode >= 500 && resp3.StatusCode <= 599) {
		isTransient = true
	}

	return &DeliveryError{
		IsTransient:  isTransient,
		HTTPCode:     resp3.StatusCode,
		ErrorMessage: fmt.Sprintf("HTTP %d: %s", resp3.StatusCode, string(bodyBytes)),
		ErrorCode:    fmt.Sprintf("HTTP_%d", resp3.StatusCode),
	}
}
