package main

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestHandleFirstPoll(t *testing.T) {
	rec := httptest.NewRecorder()
	params := debugPollParams{after: 0, effectiveLimit: 200}

	if !handleFirstPoll(rec, 1, params, time.Now()) {
		t.Fatal("expected first poll to be handled")
	}
	if rec.Code == 0 {
		t.Fatal("expected response status to be written")
	}
}

func TestHandleFirstPoll_NotFirstPoll(t *testing.T) {
	rec := httptest.NewRecorder()
	params := debugPollParams{after: 10, effectiveLimit: 200}

	if handleFirstPoll(rec, 1, params, time.Now()) {
		t.Fatal("expected non-first poll to skip helper")
	}
}
