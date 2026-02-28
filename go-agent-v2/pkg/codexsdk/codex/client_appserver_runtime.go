package codex

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

func (c *AppServerClient) pingLoop(conn *websocket.Conn) {
	ticker := time.NewTicker(appServerPingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-c.wsDone:
			return
		case <-ticker.C:
			c.wsMu.Lock()
			if c.ws != conn { c.wsMu.Unlock(); return }
			err := c.ws.WriteControl(websocket.PingMessage, []byte("ping"), time.Now().Add(appServerWriteTimeout))
			if err != nil {
				_ = c.ws.Close()
				c.ws = nil
				c.wsMu.Unlock()
				return
			}
			c.wsMu.Unlock()
		}
	}
}

func (c *AppServerClient) asWriteJSON(v any) error {
	c.wsMu.Lock()
	defer c.wsMu.Unlock()
	if c.ws == nil {
		err := apperrors.New("AppServerClient.asWriteJSON", "ws not connected")
		c.failPendingCalls(err)
		return err
	}
	_ = c.ws.SetWriteDeadline(time.Now().Add(appServerWriteTimeout))
	if err := c.ws.WriteJSON(v); err != nil {
		writeErr := apperrors.Wrap(err, "AppServerClient.asWriteJSON", "ws write")
		_ = c.ws.Close()
		c.ws = nil
		c.failPendingCalls(writeErr)
		return writeErr
	}
	return nil
}

func (c *AppServerClient) failPendingCalls(err error) {
	if err == nil {
		err = apperrors.New("AppServerClient.failPendingCalls", "connection unavailable")
	}
	c.pending.Range(func(_, value any) bool {
		if call, ok := value.(*pendingCall); ok { call.resolve(nil, err) }
		return true
	})
}

func (c *AppServerClient) SpawnAndConnect(ctx context.Context, prompt, cwd, model, instructions string, dynamicTools []DynamicTool) error {
	if err := c.Spawn(ctx); err != nil { return err }
	conn, err := c.dialWS(c.ctx)
	if err != nil { _ = c.Kill(); return apperrors.Wrap(err, "AppServerClient.connectWS", "ws connect") }
	c.replaceWSConnAndStartLoops(conn, false)
	if err := c.Initialize(); err != nil { _ = c.Kill(); return apperrors.Wrap(err, "AppServerClient.SpawnAndConnect", "initialize") }
	threadID, err := c.ThreadStart(cwd, model, instructions, dynamicTools)
	if err != nil { _ = c.Kill(); return err }
	logger.Info("codex: app-server thread started",
		logger.FieldAgentID, c.AgentID,
		logger.FieldPort, c.Port,
		logger.FieldThreadID, threadID,
		"dynamic_tools", len(dynamicTools),
	)
	return nil
}

func (c *AppServerClient) Shutdown() error {
	if c.stopped.Swap(true) { return nil }
	c.cancel()
	c.cancelStreamErrorRecoveryTimer()
	if err := c.notify("shutdown", nil); err != nil {
		logger.Debug("codex: shutdown notify failed (best-effort)",
			logger.FieldAgentID, c.AgentID, logger.FieldError, err)
	}

	c.wsMu.Lock()
	if c.ws != nil {
		closeMsg := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "shutdown")
		_ = c.ws.WriteControl(websocket.CloseMessage, closeMsg, time.Now().Add(time.Second))
		_ = c.ws.Close()
	}
	c.wsMu.Unlock()

	select {
	case <-c.wsDone:
	case <-time.After(3 * time.Second):
	}

	if err := c.Kill(); err != nil { return err }

	if c.stderrCollector != nil { _ = c.stderrCollector.Close() }
	return nil
}

func (c *AppServerClient) Kill() error {
	if c.Cmd == nil || c.Cmd.Process == nil { return nil }
	pid := c.Cmd.Process.Pid
	killErr := syscall.Kill(-pid, syscall.SIGKILL)
	if killErr != nil { killErr = c.Cmd.Process.Kill() }
	if killErr != nil && !errors.Is(killErr, os.ErrProcessDone) { return killErr }
	waitDone := make(chan error, 1)
	go func() { waitDone <- c.Cmd.Wait() }()
	select {
	case waitErr := <-waitDone:
		if waitErr == nil { return nil }
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) || strings.Contains(waitErr.Error(), "Wait was already called") || strings.Contains(waitErr.Error(), "no child processes") {
			return nil
		}
		return waitErr
	case <-time.After(5 * time.Second):
		logger.Warn("codex: Kill() Cmd.Wait timed out after 5s, abandoning",
			logger.FieldAgentID, c.AgentID,
			"pid", c.Cmd.Process.Pid,
		)
		return nil
	}
}

func (c *AppServerClient) Running() bool { return !c.stopped.Load() && c.Cmd != nil && c.Cmd.ProcessState == nil }
