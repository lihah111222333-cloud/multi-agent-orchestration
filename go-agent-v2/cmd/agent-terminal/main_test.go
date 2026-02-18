package main

import (
	"strings"
	"testing"
)

type lspSetupStub struct {
	called bool
	root   string
}

func (s *lspSetupStub) SetupLSP(rootDir string) {
	s.called = true
	s.root = rootDir
}

func TestSetupAppServerLSPRoot(t *testing.T) {
	stub := &lspSetupStub{}
	setupAppServerLSPRoot(stub)

	if !stub.called {
		t.Fatal("expected SetupLSP to be called during app server setup")
	}
	if strings.TrimSpace(stub.root) == "" {
		t.Fatal("expected non-empty root dir when calling SetupLSP")
	}
}

