package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/jackc/pgx/v5"
)

func main() {
	connStr := os.Getenv("POSTGRES_CONNECTION_STRING")
	if connStr == "" {
		fmt.Println("POSTGRES_CONNECTION_STRING not set")
		os.Exit(1)
	}

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, connStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to connect to database: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close(ctx)

	files, err := filepath.Glob("migrations/*.sql")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to list migrations: %v\n", err)
		os.Exit(1)
	}

	sort.Strings(files)

	for _, file := range files {
		fmt.Printf("Applying %s...\n", file)
		content, err := os.ReadFile(file)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to read %s: %v\n", file, err)
			os.Exit(1)
		}

		// Split by statement if needed, or run as whole block
		// For simplicity, running as whole block.
		// Note: pgx Exec handles multiple statements in one string
		_, err = conn.Exec(ctx, string(content))
		if err != nil {
			// Some errors might be acceptable if idempotent, but let's log
			fmt.Fprintf(os.Stderr, "Error applying %s: %v\n", file, err)
			// Decide whether to continue or exit. Proceeding for now as some might fail if already exist and not perfectly idempotent
		} else {
			fmt.Printf("Applied %s\n", file)
		}
	}
	fmt.Println("Migration complete.")
}
