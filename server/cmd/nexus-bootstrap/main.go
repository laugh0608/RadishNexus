package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	authpostgres "github.com/laugh0608/RadishNexus/server/internal/platform/authn/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/platform/runtimeconfig"
)

const maxCredentialInputBytes = 4096

type options struct {
	displayName      string
	workspaceName    string
	credentialsStdin bool
}

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		log.Printf("radishnexus identity bootstrap failed: %v", err)
		os.Exit(1)
	}
}

func run(args []string, stdin io.Reader, stdout io.Writer) error {
	parsed, err := parseOptions(args)
	if err != nil {
		return err
	}
	credentials, err := readCredentials(stdin)
	if err != nil {
		return err
	}
	databaseURL, err := runtimeconfig.DatabaseURL(os.Getenv, os.ReadFile)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return errors.New("DATABASE_URL is invalid")
	}
	poolConfig.ConnConfig.RuntimeParams["application_name"] = "radishnexus-bootstrap"
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return fmt.Errorf("connect bootstrap database: %w", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping bootstrap database: %w", err)
	}

	service := authn.NewService(
		authpostgres.New(pool),
		authn.NewArgon2idHasher(),
		authn.CryptoSecretGenerator{},
		authn.SystemClock{},
	)
	result, err := service.Bootstrap(ctx, authn.BootstrapInput{
		Email:         credentials.Email,
		DisplayName:   parsed.displayName,
		WorkspaceName: parsed.workspaceName,
		Password:      credentials.Password,
	})
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(
		stdout,
		"local identity bootstrapped: user_id=%s workspace_id=%s\n",
		result.UserID,
		result.WorkspaceID,
	); err != nil {
		return fmt.Errorf("write bootstrap result: %w", err)
	}
	return nil
}

func parseOptions(args []string) (options, error) {
	flags := flag.NewFlagSet("nexus-bootstrap", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var parsed options
	flags.StringVar(&parsed.displayName, "display-name", "", "first administrator display name")
	flags.StringVar(&parsed.workspaceName, "workspace-name", "", "first Workspace name")
	flags.BoolVar(&parsed.credentialsStdin, "credentials-stdin", false, "read email and password JSON from standard input")
	if err := flags.Parse(args); err != nil {
		return options{}, errors.New("invalid bootstrap options")
	}
	if flags.NArg() != 0 {
		return options{}, fmt.Errorf("bootstrap does not accept positional arguments")
	}
	if parsed.displayName == "" || parsed.workspaceName == "" {
		return options{}, fmt.Errorf("--display-name and --workspace-name are required")
	}
	if !parsed.credentialsStdin {
		return options{}, fmt.Errorf("--credentials-stdin is required; credentials must not be command arguments")
	}
	return parsed, nil
}

func readCredentials(stdin io.Reader) (authn.LoginInput, error) {
	body, err := io.ReadAll(io.LimitReader(stdin, maxCredentialInputBytes+1))
	if err != nil || len(body) > maxCredentialInputBytes {
		return authn.LoginInput{}, errors.New("cannot read bounded credential input")
	}
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return authn.LoginInput{}, errors.New("credentials must be a JSON object with email and password")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return authn.LoginInput{}, errors.New("credentials must be one JSON object")
	}
	if input.Email == "" || input.Password == "" {
		return authn.LoginInput{}, errors.New("email and password are required")
	}
	return authn.LoginInput{Email: input.Email, Password: input.Password}, nil
}
