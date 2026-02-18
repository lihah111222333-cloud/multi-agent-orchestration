package store

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func getTestPool(t *testing.T) *pgxpool.Pool {
	connStr := os.Getenv("TEST_POSTGRES_CONNECTION_STRING")
	if connStr == "" {
		t.Skip("TEST_POSTGRES_CONNECTION_STRING not set")
	}
	config, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatalf("connect to db: %v", err)
	}
	return pool
}

func TestUIPreferenceStore(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	store := NewUIPreferenceStore(pool)
	ctx := context.Background()

	// Ensure clean state
	pool.Exec(ctx, "DELETE FROM ui_preferences WHERE key LIKE 'test.%'")

	t.Run("Get_NonExistent_ReturnsEmpty", func(t *testing.T) {
		val, err := store.Get(ctx, "test.nonexistent")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if val != nil {
			t.Errorf("expected nil, got %v", val)
		}
	})

	t.Run("Set_Then_Get", func(t *testing.T) {
		key := "test.key1"
		value := map[string]any{"foo": "bar", "num": 123}

		if err := store.Set(ctx, key, value); err != nil {
			t.Fatalf("failed to set: %v", err)
		}

		got, err := store.Get(ctx, key)
		if err != nil {
			t.Fatalf("failed to get: %v", err)
		}

		// JSON unmarshals numbers as float64
		m, ok := got.(map[string]any)
		if !ok {
			t.Fatalf("expected map, got %T", got)
		}
		if m["foo"] != "bar" {
			t.Errorf("expected foo=bar, got %v", m["foo"])
		}
		if m["num"].(float64) != 123 {
			t.Errorf("expected num=123, got %v", m["num"])
		}
	})

	t.Run("Set_Overwrite", func(t *testing.T) {
		key := "test.key2"
		val1 := "value1"
		val2 := "value2"

		store.Set(ctx, key, val1)
		store.Set(ctx, key, val2)

		got, err := store.Get(ctx, key)
		if err != nil {
			t.Fatalf("failed to get: %v", err)
		}
		if got != val2 {
			t.Errorf("expected %v, got %v", val2, got)
		}
	})

	t.Run("GetAll", func(t *testing.T) {
		pool.Exec(ctx, "DELETE FROM ui_preferences WHERE key LIKE 'test.all.%'")
		store.Set(ctx, "test.all.1", 1)
		store.Set(ctx, "test.all.2", 2)

		all, err := store.GetAll(ctx)
		if err != nil {
			t.Fatalf("failed to get all: %v", err)
		}

		if all["test.all.1"].(float64) != 1 {
			t.Errorf("expected test.all.1=1")
		}
		if all["test.all.2"].(float64) != 2 {
			t.Errorf("expected test.all.2=2")
		}
	})
}
