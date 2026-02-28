// server_lifecycle.go — 服务启动与运行时清理。
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

// ListenAndServe 启动 WebSocket 服务器。
//
// addr 格式: "ws://127.0.0.1:4500" 或 "127.0.0.1:4500"。
func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
	defer s.cleanupRuntimeResources()

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// 解析地址: 去掉 ws:// / wss:// 前缀
	host := strings.TrimPrefix(strings.TrimPrefix(addr, "ws://"), "wss://")

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { handleUpgrade(s, w, r) })    // WebSocket
	mux.HandleFunc("/rpc", func(w http.ResponseWriter, r *http.Request) { handleHTTPRPC(s, w, r) }) // HTTP JSON-RPC (调试模式)
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) { handleSSE(s, w, r) })  // SSE 事件流 (调试模式)

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

	// 优雅关闭: 给活跃连接 5 秒完成处理
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

		// 防止 uiThrottle 定时器泄漏 + 清理积累的状态 map。
		stopAllUIThrottleTimersState(s)
		clearAllToolCallState(s)
	})
}
