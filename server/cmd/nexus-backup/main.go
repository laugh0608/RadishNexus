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

	"github.com/laugh0608/RadishNexus/server/internal/backuprestore"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	if err := run(ctx, os.Args[1:], os.Getenv, os.Stdout); err != nil {
		log.Printf("radishnexus backup failed: %v", err)
		os.Exit(1)
	}
}

func run(
	ctx context.Context,
	args []string,
	getenv func(string) string,
	stdout io.Writer,
) error {
	flags := flag.NewFlagSet("nexus-backup", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var outputDirectory string
	flags.StringVar(&outputDirectory, "output", "", "new directory for the completed backup")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse backup arguments: %w", err)
	}
	if flags.NArg() != 0 {
		return errors.New("nexus-backup does not accept positional arguments")
	}
	if outputDirectory == "" {
		return errors.New("--output is required")
	}
	databaseURL := getenv("DATABASE_URL")
	if databaseURL == "" {
		return errors.New("DATABASE_URL is required")
	}

	config, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		return fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	config.RuntimeParams["application_name"] = "radishnexus-backup-preflight"
	connection, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		return fmt.Errorf("connect backup source: %w", err)
	}
	defer func() {
		if closeErr := connection.Close(context.Background()); closeErr != nil {
			log.Printf("close backup source connection: %v", closeErr)
		}
	}()

	manifest, err := backuprestore.NewService().Backup(ctx, connection, databaseURL, outputDirectory)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(
		stdout,
		"backup completed: %s (format %d, PostgreSQL %d)\n",
		outputDirectory,
		manifest.FormatVersion,
		manifest.PostgreSQLMajor,
	)
	return err
}
