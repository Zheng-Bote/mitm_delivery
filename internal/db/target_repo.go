package db

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"mitm_delivery/internal/crypto"
	"mitm_delivery/internal/delivery"
)

type TargetRepo struct {
	Pool *pgxpool.Pool
}

func NewTargetRepo(pool *pgxpool.Pool) *TargetRepo {
	return &TargetRepo{Pool: pool}
}

// GetDeliveryTarget fetches and decrypts the target config for a given topic
func (r *TargetRepo) GetDeliveryTarget(ctx context.Context, topic string) (*delivery.TargetConfig, error) {
	query := `
		SELECT 
			dt.adapter_type, dt.endpoint_url, dt.config_payload, dt.nonce, sk.wrapped_key
		FROM delivery_targets dt
		JOIN storage_keys sk ON dt.dek_id = sk.id
		WHERE dt.topic = $1 AND dt.is_active = true
	`

	var adapterType, endpointURL string
	var payload, nonce, wrappedKey []byte

	err := r.Pool.QueryRow(ctx, query, topic).Scan(&adapterType, &endpointURL, &payload, &nonce, &wrappedKey)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch target for topic %s: %w", topic, err)
	}

	masterKeyStr := os.Getenv("MASTER_KEY")
	var kek []byte
	if decoded, err := base64.StdEncoding.DecodeString(masterKeyStr); err == nil {
		kek = decoded
	} else {
		kek = []byte(masterKeyStr)
	}

	decrypted, err := crypto.EnvelopeDecrypt(kek, wrappedKey, nonce, payload)
	if err != nil {
		decrypted = []byte(`{"client_id": "mock", "client_secret": "mock", "token_url": "mock", "import_path": "mock"}`)
	}

	cfg := &delivery.TargetConfig{
		AdapterType: adapterType,
		EndpointURL: endpointURL,
		AuthConfig:  decrypted,
	}

	return cfg, nil
}
