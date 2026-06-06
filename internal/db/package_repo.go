package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Package struct {
	ID             string
	Payload        []byte
	Status         string
	RetryCount     int
	IdempotencyKey string
	ErrorMessage   *string
	CreatedAt      time.Time
	DeliveredAt    *time.Time
	NextRetryAt    *time.Time
}

type PackageRepo struct {
	pool *pgxpool.Pool
}

func NewPackageRepo(pool *pgxpool.Pool) *PackageRepo {
	return &PackageRepo{pool: pool}
}

// ClaimPendingPackages claims packages that are 'pending' or 'failed' and ready for retry.
func (r *PackageRepo) ClaimPendingPackages(ctx context.Context, limit int) ([]Package, error) {
	query := `
		UPDATE packages
		SET status = 'sending'
		WHERE id IN (
			SELECT id FROM packages
			WHERE status = 'pending' 
			   OR (status = 'failed' AND next_retry_at <= NOW())
			ORDER BY created_at ASC
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING id::text, payload, status, retry_count, idempotency_key::text, error_message, created_at, delivered_at, next_retry_at
	`
	rows, err := r.pool.Query(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to claim packages: %w", err)
	}
	defer rows.Close()

	var packages []Package
	for rows.Next() {
		var p Package
		if err := rows.Scan(
			&p.ID, &p.Payload, &p.Status, &p.RetryCount,
			&p.IdempotencyKey, &p.ErrorMessage, &p.CreatedAt, &p.DeliveredAt, &p.NextRetryAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan package row: %w", err)
		}
		packages = append(packages, p)
	}
	return packages, rows.Err()
}

// UpdateStatusDelivered marks a package as successfully delivered.
func (r *PackageRepo) UpdateStatusDelivered(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `UPDATE packages SET status = 'delivered', delivered_at = NOW() WHERE id = $1`, id)
	return err
}

// UpdateStatusFailed updates a package to failed state and schedules its next retry.
func (r *PackageRepo) UpdateStatusFailed(ctx context.Context, id string, errMsg string, nextRetryAt time.Time, retryCount int) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE packages 
		SET status = 'failed', error_message = $1, next_retry_at = $2, retry_count = $3 
		WHERE id = $4
	`, errMsg, nextRetryAt, retryCount, id)
	return err
}
