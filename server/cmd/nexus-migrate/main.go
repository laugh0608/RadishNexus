package main

import (
	"context"
	"errors"
	"log"
	"os"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/laugh0608/RadishNexus/server/db"
)

func main() {
	if err := run(); err != nil {
		log.Printf("radishnexus migration failed: %v", err)
		os.Exit(1)
	}
}

func run() error {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return errors.New("DATABASE_URL is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	config, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		return err
	}
	config.RuntimeParams["application_name"] = "radishnexus-migrate"
	connection, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := connection.Close(context.Background()); closeErr != nil {
			log.Printf("close migration connection: %v", closeErr)
		}
	}()

	return db.Migrate(ctx, connection)
}
