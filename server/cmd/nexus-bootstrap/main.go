package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	authpostgres "github.com/laugh0608/RadishNexus/server/internal/platform/authn/postgres"
)

const maxPasswordInputBytes = 1026

type options struct {
	loginName     string
	displayName   string
	workspaceName string
	passwordStdin bool
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
	password, err := readPassword(stdin)
	if err != nil {
		return err
	}
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return errors.New("DATABASE_URL is required")
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
		LoginName:     parsed.loginName,
		DisplayName:   parsed.displayName,
		WorkspaceName: parsed.workspaceName,
		Password:      password,
	})
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(
		stdout,
		"local identity bootstrapped: user_id=%s workspace_id=%s login_name=%s\n",
		result.UserID,
		result.WorkspaceID,
		result.LoginName,
	); err != nil {
		return fmt.Errorf("write bootstrap result: %w", err)
	}
	return nil
}

func parseOptions(args []string) (options, error) {
	flags := flag.NewFlagSet("nexus-bootstrap", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var parsed options
	flags.StringVar(&parsed.loginName, "login", "", "lowercase local login name")
	flags.StringVar(&parsed.displayName, "display-name", "", "first administrator display name")
	flags.StringVar(&parsed.workspaceName, "workspace-name", "", "first Workspace name")
	flags.BoolVar(&parsed.passwordStdin, "password-stdin", false, "read the password from standard input")
	if err := flags.Parse(args); err != nil {
		return options{}, fmt.Errorf("parse bootstrap options: %w", err)
	}
	if flags.NArg() != 0 {
		return options{}, fmt.Errorf("bootstrap does not accept positional arguments")
	}
	if parsed.loginName == "" || parsed.displayName == "" || parsed.workspaceName == "" {
		return options{}, fmt.Errorf("--login, --display-name, and --workspace-name are required")
	}
	if !parsed.passwordStdin {
		return options{}, fmt.Errorf("--password-stdin is required; passwords must not be command arguments")
	}
	return parsed, nil
}

func readPassword(stdin io.Reader) (string, error) {
	body, err := io.ReadAll(io.LimitReader(stdin, maxPasswordInputBytes))
	if err != nil {
		return "", fmt.Errorf("read bootstrap password: %w", err)
	}
	if len(body) == maxPasswordInputBytes {
		return "", fmt.Errorf("bootstrap password input exceeds 1025 bytes including line ending")
	}
	password := string(body)
	password = strings.TrimSuffix(password, "\n")
	password = strings.TrimSuffix(password, "\r")
	if strings.ContainsAny(password, "\r\n") {
		return "", fmt.Errorf("bootstrap password must be provided as exactly one line")
	}
	return password, nil
}
