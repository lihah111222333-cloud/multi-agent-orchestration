package apiserver

import (
	"context"
	"net"
	"net/http"
	"strings"
	"time"

	pkgerr "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
	defer s.cleanupRuntimeResources()

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	host := strings.TrimPrefix(strings.TrimPrefix(addr, "ws://"), "wss://")

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { handleUpgrade(s, w, r) })
	mux.HandleFunc("/rpc", func(w http.ResponseWriter, r *http.Request) { handleHTTPRPC(s, w, r) })
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) { handleSSE(s, w, r) })

	srv := &http.Server{
		Addr:              host,
		Handler:           recoveryMiddleware(corsMiddleware(mux)),
		BaseContext:       func(_ net.Listener) context.Context { return runCtx },
		ReadHeaderTimeout: 10 * time.Second,
	}

	ln, err := net.Listen("tcp", host)
	if err != nil {
		return pkgerr.Wrap(err, "Server.ListenAndServe", "listen")
	}
	defer ln.Close()

	util.SafeGo(func() {
		<-runCtx.Done()
		logger.Info("app-server: shutting down")
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if shutdownErr := srv.Shutdown(shutdownCtx); shutdownErr != nil && shutdownErr != http.ErrServerClosed {
			logger.Warn("app-server: shutdown error", logger.FieldError, shutdownErr)
			return
		}
		logger.Info("app-server: shutdown completed")
	})

	logger.Info("app-server: listening", logger.FieldAddr, host)
	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		return pkgerr.Wrap(err, "Server.ListenAndServe", "listen")
	}
	return nil
}

func (s *Server) cleanupRuntimeResources() {
	if s == nil {
		return
	}
	doRuntimeCleanupState(s, func() {
		cancelAllCodeRuns(s)
		if runner := s.codeRunner; runner != nil {
			runner.Cleanup()
		}
		clearAllAgentWorkDirsState(s)

		stopAllUIThrottleTimersState(s)
		clearAllToolCallState(s)
	})
}
