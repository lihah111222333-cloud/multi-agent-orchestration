package database

import (
	"context"
	"testing"
)

func TestLoadAppliedVersions_NilPool(t *testing.T) {
	_, err := loadAppliedVersions(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil pool")
	}
}

func TestApplyOneMigration_NilPool(t *testing.T) {
	err := applyOneMigration(context.Background(), nil, t.TempDir(), "001_init.sql")
	if err == nil {
		t.Fatal("expected error for nil pool")
	}
}
