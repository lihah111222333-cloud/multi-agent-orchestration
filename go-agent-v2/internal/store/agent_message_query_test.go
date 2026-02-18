package store

import (
	"strings"
	"testing"
)

func TestBuildListDistinctAgentIDsQuery_Unlimited(t *testing.T) {
	sql, args := buildListDistinctAgentIDsQuery(0)

	if strings.Contains(strings.ToUpper(sql), "LIMIT") {
		t.Fatalf("expected unlimited query without LIMIT, got: %s", sql)
	}
	if len(args) != 0 {
		t.Fatalf("expected no args for unlimited query, got: %d", len(args))
	}
}

func TestBuildListDistinctAgentIDsQuery_WithLimit(t *testing.T) {
	sql, args := buildListDistinctAgentIDsQuery(128)

	if !strings.Contains(strings.ToUpper(sql), "LIMIT $1") {
		t.Fatalf("expected LIMIT $1 in query, got: %s", sql)
	}
	if len(args) != 1 {
		t.Fatalf("expected 1 arg for limited query, got: %d", len(args))
	}
	if got := args[0]; got != 128 {
		t.Fatalf("expected limit arg 128, got: %#v", got)
	}
}
