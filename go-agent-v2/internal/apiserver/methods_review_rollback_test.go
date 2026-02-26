package apiserver

import (
	"context"
	"strings"
	"testing"
)

func TestBuildReviewStartArgsValidation(t *testing.T) {
	_, err := buildReviewStartArgs(reviewStartParams{Target: reviewTarget{}})
	if err == nil || !strings.Contains(err.Error(), "target.type is required") {
		t.Fatalf("buildReviewStartArgs missing target.type err = %v", err)
	}

	_, err = buildReviewStartArgs(reviewStartParams{Target: reviewTarget{Type: "custom"}})
	if err == nil || !strings.Contains(err.Error(), "target.instructions is required") {
		t.Fatalf("buildReviewStartArgs missing custom instructions err = %v", err)
	}

	args, err := buildReviewStartArgs(reviewStartParams{Target: reviewTarget{Type: "custom", Instructions: "review this"}})
	if err != nil {
		t.Fatalf("buildReviewStartArgs custom err = %v", err)
	}
	if args != "review this" {
		t.Fatalf("buildReviewStartArgs custom args = %q, want %q", args, "review this")
	}
}

func TestReviewStartTypedRequiresThreadAndTarget(t *testing.T) {
	s := &Server{}
	_, err := s.reviewStartTyped(context.Background(), reviewStartParams{})
	if err == nil || !strings.Contains(err.Error(), "threadId is required") {
		t.Fatalf("reviewStartTyped missing threadId err = %v", err)
	}

	_, err = s.reviewStartTyped(context.Background(), reviewStartParams{ThreadID: "thread-1"})
	if err == nil || !strings.Contains(err.Error(), "target.type is required") {
		t.Fatalf("reviewStartTyped missing target err = %v", err)
	}
}

func TestThreadRollbackTypedRequiresPositiveNumTurns(t *testing.T) {
	s := &Server{}
	_, err := s.threadRollbackTyped(context.Background(), threadRollbackParams{ThreadID: "thread-1"})
	if err == nil || !strings.Contains(err.Error(), "numTurns must be >= 1") {
		t.Fatalf("threadRollbackTyped missing numTurns err = %v", err)
	}

	zero := 0
	_, err = s.threadRollbackTyped(context.Background(), threadRollbackParams{ThreadID: "thread-1", NumTurns: &zero})
	if err == nil || !strings.Contains(err.Error(), "numTurns must be >= 1") {
		t.Fatalf("threadRollbackTyped zero numTurns err = %v", err)
	}
}
