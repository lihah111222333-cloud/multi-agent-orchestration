package apiserver

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

func handleHTTPRPC(s *Server, w http.ResponseWriter, r *http.Request) {
	if s == nil {
		http.Error(w, "server not ready", http.StatusServiceUnavailable)
		return
	}
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      any             `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONRPCError(w, nil, CodeParseError, "parse error: "+err.Error())
		return
	}
	if req.JSONRPC != jsonrpcVersion {
		writeJSONRPCError(w, req.ID, CodeInvalidRequest, "invalid request: jsonrpc must be \"2.0\"")
		return
	}
	if req.Method == "" {
		writeJSONRPCError(w, req.ID, CodeInvalidRequest, "invalid request: method is required")
		return
	}

	params := req.Params
	if len(params) == 0 || string(params) == "null" {
		params = json.RawMessage("{}")
	}

	resp := dispatchRequest(s, r.Context(), req.ID, req.Method, params)
	if resp == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if resp.Error != nil {
		writeJSONRPCError(w, req.ID, resp.Error.Code, resp.Error.Message)
		return
	}

	result := map[string]any{
		"jsonrpc": jsonrpcVersion,
		"id":      req.ID,
		"result":  resp.Result,
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(result); err != nil {
		logger.Warn("http-rpc: encode response failed", logger.FieldError, err)
	}
}

func writeJSONRPCError(w http.ResponseWriter, id any, code int, message string) {
	resp := map[string]any{
		"jsonrpc": jsonrpcVersion,
		"id":      id,
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		logger.Warn("http-rpc: encode error response failed", logger.FieldError, err)
	}
}

func recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rv := recover(); rv != nil {
				logger.Error("http: handler panicked",
					logger.FieldMethod, r.Method,
					logger.FieldPath, r.URL.Path,
					logger.FieldRemote, r.RemoteAddr,
					logger.FieldError, rv,
				)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func handleSSE(s *Server, w http.ResponseWriter, r *http.Request) {
	if s == nil {
		http.Error(w, "server not ready", http.StatusServiceUnavailable)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ch := make(chan []byte, 64)

	addSSEClientState(s, ch)

	defer func() {
		removeSSEClientState(s, ch)
	}()

	logger.Info("sse: client connected", logger.FieldRemote, r.RemoteAddr)

	for {
		select {
		case data := <-ch:
			_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		case <-r.Context().Done():
			logger.Info("sse: client disconnected", logger.FieldRemote, r.RemoteAddr)
			return
		}
	}
}
