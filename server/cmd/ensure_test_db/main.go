package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "ensure_test_db: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return errors.New("DATABASE_URL is not set; refusing to guess a database to create")
	}

	cfg, err := pgx.ParseConfig(dbURL)
	if err != nil {
		return fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	target := cfg.Database
	if !strings.HasSuffix(target, "_test") {
		return fmt.Errorf("database %q does not end in _test; refusing to create a non-test database", target)
	}

	adminCfg := cfg.Copy()
	adminCfg.Database = "postgres"
	conn, err := pgx.ConnectConfig(ctx, adminCfg)
	if err != nil {
		return fmt.Errorf("connect to maintenance database: %w", err)
	}
	defer func() {
		if closeErr := conn.Close(ctx); closeErr != nil {
			fmt.Fprintf(os.Stderr, "ensure_test_db: close connection: %v\n", closeErr)
		}
	}()

	var exists bool
	err = conn.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)", target).Scan(&exists)
	if err != nil {
		return fmt.Errorf("check database existence: %w", err)
	}
	if exists {
		fmt.Printf("ensure_test_db: database %q already exists\n", target)
		return nil
	}

	_, err = conn.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{target}.Sanitize())
	if err != nil {

		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "42P04" {
			fmt.Printf("ensure_test_db: database %q was created concurrently\n", target)
			return nil
		}
		return fmt.Errorf("create database %q: %w", target, err)
	}
	fmt.Printf("ensure_test_db: created database %q\n", target)
	return nil
}
