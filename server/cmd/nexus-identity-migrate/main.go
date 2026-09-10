// nexus-identity-migrate assigns operator-reviewed emails to legacy accounts.
// It never resets passwords or changes stable user IDs or permissions.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	authpostgres "github.com/laugh0608/RadishNexus/server/internal/platform/authn/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/platform/runtimeconfig"
)

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, input io.Reader, output io.Writer) error {
	if len(args) != 1 || args[0] != "--mapping-stdin" {
		return errors.New("use nexus-identity-migrate --mapping-stdin; identity mappings must not be command arguments")
	}
	mappings, err := readMappings(input)
	if err != nil {
		return err
	}
	databaseURL, err := runtimeconfig.DatabaseURL(os.Getenv, os.ReadFile)
	if err != nil {
		return err
	}
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return errors.New("invalid database configuration")
	}
	config.ConnConfig.RuntimeParams["application_name"] = "radishnexus-identity-migrate"
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return errors.New("cannot connect identity migration database")
	}
	defer pool.Close()
	if err := authpostgres.New(pool).MapLegacyEmails(ctx, mappings); err != nil {
		return err
	}
	_, err = fmt.Fprintf(output, "mapped %d legacy accounts\n", len(mappings))
	return err
}

func readMappings(input io.Reader) ([]authpostgres.EmailMapping, error) {
	body, err := io.ReadAll(io.LimitReader(input, 1<<20+1))
	if err != nil || len(body) > 1<<20 {
		return nil, errors.New("cannot read bounded identity mapping")
	}
	var mappings []authpostgres.EmailMapping
	// Reuse a strict decoder without ever returning its potentially sensitive error.
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&mappings); err != nil {
		return nil, errors.New("mapping must be a JSON array of user_id and email")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, errors.New("mapping must contain one JSON array")
	}
	if len(mappings) == 0 || len(mappings) > 1000 {
		return nil, errors.New("mapping requires 1-1000 accounts")
	}
	return mappings, nil
}
