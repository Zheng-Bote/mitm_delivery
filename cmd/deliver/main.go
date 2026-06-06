package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mitm_delivery/internal/db"
	"mitm_delivery/internal/delivery"
	"mitm_delivery/internal/engine"
)

type JobArgs struct {
	Workers      int                   `json:"workers"`
	BatchSize    int                   `json:"batch_size"`
	MaxRetries   int                   `json:"max_retries"`
	TargetConfig delivery.TargetConfig `json:"target_config"`
}

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

func main() {
	if len(os.Args) < 2 {
		log.Fatalf("Usage: %s <job_args_json>", os.Args[0])
	}

	// 1. Parse Job Arguments (os.Args[1])
	var jobArgs JobArgs
	if err := json.Unmarshal([]byte(os.Args[1]), &jobArgs); err != nil {
		log.Fatalf("Failed to parse JobArgs JSON from os.Args[1]: %v", err)
	}

	if jobArgs.Workers <= 0 {
		jobArgs.Workers = 5
	}
	if jobArgs.BatchSize <= 0 {
		jobArgs.BatchSize = 100
	}
	if jobArgs.MaxRetries <= 0 {
		jobArgs.MaxRetries = 5
	}

	// 2. Database Connection from ENV
	dbHost := getEnv("DB_HOST", getEnv("PGHOST", "localhost"))
	dbPort := getEnv("DB_PORT", getEnv("PGPORT", "5432"))
	dbUser := getEnv("DB_USER", getEnv("PGUSER", "postgres"))
	dbPass := getEnv("DB_PASS", getEnv("PGPASSWORD", ""))
	dbName := getEnv("DB_NAME", getEnv("PGDATABASE", "postgres"))

	connString := fmt.Sprintf("postgres://%s:%s@%s:%s/%s", dbUser, dbPass, dbHost, dbPort, dbName)
	pool, err := pgxpool.New(context.Background(), connString)
	if err != nil {
		log.Fatalf("Unable to connect to database: %v\n", err)
	}
	defer pool.Close()

	// 3. Initialize Repositories and Engine
	pkgRepo := db.NewPackageRepo(pool)
	dlqRepo := db.NewDLQRepo(pool)
	retryEngine := engine.NewRetryEngine(pkgRepo, dlqRepo, jobArgs.MaxRetries)

	// 4. Instantiate Delivery Sender
	var sender delivery.DeliverySender
	switch jobArgs.TargetConfig.AdapterType {
	case "SAAS":
		sender = delivery.NewSaaSAdapter(nil)
	case "APIGEE":
		sender = delivery.NewApigeeAdapter(nil)
	default:
		log.Fatalf("Unsupported adapter_type: %s", jobArgs.TargetConfig.AdapterType)
	}

	// Context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		log.Println("Shutting down gracefully...")
		cancel()
	}()

	log.Printf("Starting Delivery Batch Job (Workers: %d, Batch Size: %d)...", jobArgs.Workers, jobArgs.BatchSize)

	// 5. Worker Pool Setup
	jobs := make(chan db.Package, jobArgs.BatchSize)
	var wg sync.WaitGroup

	// Start workers
	for i := 0; i < jobArgs.Workers; i++ {
		wg.Add(1)
		go worker(ctx, &wg, jobs, retryEngine, sender, jobArgs.TargetConfig)
	}

	totalProcessed := 0

	// 6. Dispatcher Loop
dispatcherLoop:
	for {
		if ctx.Err() != nil {
			break dispatcherLoop
		}

		packages, err := pkgRepo.ClaimPendingPackages(ctx, jobArgs.BatchSize)
		if err != nil {
			log.Printf("Error claiming packages: %v", err)
			time.Sleep(2 * time.Second)
			continue
		}

		if len(packages) == 0 {
			log.Println("No more packages to deliver. Batch job complete.")
			break dispatcherLoop
		}

		log.Printf("Claimed batch of %d packages", len(packages))
		for _, p := range packages {
			select {
			case jobs <- p:
			case <-ctx.Done():
				break dispatcherLoop
			}
		}
		totalProcessed += len(packages)
	}

	close(jobs)
	wg.Wait()
	log.Printf("Delivery Batch Job finished successfully. Processed %d records.", totalProcessed)
}

func worker(ctx context.Context, wg *sync.WaitGroup, jobs <-chan db.Package, engine *engine.RetryEngine, sender delivery.DeliverySender, config delivery.TargetConfig) {
	defer wg.Done()
	for pkg := range jobs {
		if err := engine.ProcessPackage(ctx, pkg, sender, config); err != nil {
			log.Printf("Failed to process package %s: %v", pkg.ID, err)
		}
	}
}
