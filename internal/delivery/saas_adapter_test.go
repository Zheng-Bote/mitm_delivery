package delivery_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"mitm_delivery/internal/delivery"
)

func TestSaaSAdapter_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Idempotency-Key") != "idem-123" {
			t.Errorf("missing or invalid Idempotency-Key header")
		}
		if r.Header.Get("Authorization") != "Bearer secret-token" {
			t.Errorf("missing or invalid Authorization header")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	authCfg, _ := json.Marshal(delivery.SaaSAuthConfig{BearerToken: "secret-token"})
	config := delivery.TargetConfig{
		EndpointURL: ts.URL,
		AuthConfig:  authCfg,
	}

	adapter := delivery.NewSaaSAdapter(ts.Client())
	err := adapter.Send(context.Background(), config, "idem-123", []byte(`{"test":"ok"}`))

	if err != nil {
		t.Fatalf("expected nil, got error: %v", err)
	}
}

func TestSaaSAdapter_TransientError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer ts.Close()

	authCfg, _ := json.Marshal(delivery.SaaSAuthConfig{APIKey: "key-123"})
	config := delivery.TargetConfig{
		EndpointURL: ts.URL,
		AuthConfig:  authCfg,
	}

	adapter := delivery.NewSaaSAdapter(ts.Client())
	err := adapter.Send(context.Background(), config, "idem-123", []byte(`{}`))

	if err == nil {
		t.Fatalf("expected error, got nil")
	}

	devErr, ok := err.(*delivery.DeliveryError)
	if !ok {
		t.Fatalf("expected DeliveryError, got %T", err)
	}

	if !devErr.IsTransient {
		t.Errorf("expected error to be transient (429 Too Many Requests)")
	}
	if devErr.HTTPCode != http.StatusTooManyRequests {
		t.Errorf("expected HTTPCode %d, got %d", http.StatusTooManyRequests, devErr.HTTPCode)
	}
}

func TestSaaSAdapter_FatalError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer ts.Close()

	authCfg, _ := json.Marshal(delivery.SaaSAuthConfig{})
	config := delivery.TargetConfig{
		EndpointURL: ts.URL,
		AuthConfig:  authCfg,
	}

	adapter := delivery.NewSaaSAdapter(ts.Client())
	err := adapter.Send(context.Background(), config, "idem-123", []byte(`{}`))

	if err == nil {
		t.Fatalf("expected error, got nil")
	}

	devErr, ok := err.(*delivery.DeliveryError)
	if !ok {
		t.Fatalf("expected DeliveryError, got %T", err)
	}

	if devErr.IsTransient {
		t.Errorf("expected error to be fatal (400 Bad Request)")
	}
	if devErr.ErrorCode != "HTTP_400" {
		t.Errorf("expected ErrorCode HTTP_400, got %s", devErr.ErrorCode)
	}
}
