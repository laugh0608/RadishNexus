package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/laugh0608/RadishNexus/server/internal/backuprestore"
	goldenpostgres "github.com/laugh0608/RadishNexus/server/internal/goldenpath/postgres"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	if err := run(ctx, os.Args[1:], os.Getenv, os.Stdout); err != nil {
		log.Printf("radishnexus restore failed: %v", err)
		os.Exit(1)
	}
}

func run(
	ctx context.Context,
	args []string,
	getenv func(string) string,
	stdout io.Writer,
) error {
	flags := flag.NewFlagSet("nexus-restore", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var inputDirectory string
	flags.StringVar(&inputDirectory, "input", "", "completed backup directory to restore")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse restore arguments: %w", err)
	}
	if flags.NArg() != 0 {
		return errors.New("nexus-restore does not accept positional arguments")
	}
	if inputDirectory == "" {
		return errors.New("--input is required")
	}
	databaseURL := getenv("DATABASE_URL")
	if databaseURL == "" {
		return errors.New("DATABASE_URL is required")
	}

	config, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		return fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	config.RuntimeParams["application_name"] = "radishnexus-restore-preflight"
	connection, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		return fmt.Errorf("connect restore target: %w", err)
	}
	defer func() {
		if closeErr := connection.Close(context.Background()); closeErr != nil {
			log.Printf("close restore target connection: %v", closeErr)
		}
	}()

	manifest, err := backuprestore.NewService().Restore(ctx, connection, databaseURL, inputDirectory)
	if err != nil {
		return err
	}
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return fmt.Errorf("parse restored database pool config: %w", err)
	}
	poolConfig.ConnConfig.RuntimeParams["application_name"] = "radishnexus-restore-activity-rebuild"
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return fmt.Errorf("connect restored database for Activity rebuild: %w", err)
	}
	defer pool.Close()
	projected, err := goldenpostgres.New(pool).RebuildActivityProjection(ctx)
	if err != nil {
		return fmt.Errorf("rebuild restored Activity projection: %w", err)
	}

	_, err = fmt.Fprintf(
		stdout,
		"restore completed: format %d, PostgreSQL %d, Activity rows %d\n",
		manifest.FormatVersion,
		manifest.PostgreSQLMajor,
		projected,
	)
	return err
}
