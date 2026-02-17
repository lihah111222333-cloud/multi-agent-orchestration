package codex

import (
	"context"
	"encoding/json"
)

// CodexClient codex 客户端接口 — 统一 http-api (Client) 和 app-server (AppServerClient)。
//
// AgentProcess.Client 使用此接口, 允许根据是否需要 dynamicTools 选择传输层。
type CodexClient interface {
	// GetPort 返回端口号。
	GetPort() int
	// GetThreadID 返回当前 thread ID。
	GetThreadID() string

	// SetEventHandler 注册事件回调。
	SetEventHandler(h EventHandler)
	// SpawnAndConnect 一键启动: spawn → 连接 → 创建 thread。
	SpawnAndConnect(ctx context.Context, prompt, cwd, model string, dynamicTools []DynamicTool) error

	// Submit 发送用户 prompt (可附带 outputSchema 约束输出格式)。
	Submit(prompt string, images, files []string, outputSchema json.RawMessage) error
	// SendCommand 发送斜杠命令。
	SendCommand(cmd, args string) error
	// SendDynamicToolResult 回传动态工具执行结果 (requestID = codex server request ID)。
	SendDynamicToolResult(callID, output string, requestID *int64) error

	// ListThreads 获取线程列表。
	ListThreads() ([]ThreadInfo, error)
	// ResumeThread 恢复已有会话。
	ResumeThread(req ResumeThreadRequest) error
	// ForkThread 分叉会话。
	ForkThread(req ForkThreadRequest) (*ForkThreadResponse, error)

	// Shutdown 优雅关闭。
	Shutdown() error
	// Kill 强制终止。
	Kill() error
	// Running 返回是否运行中。
	Running() bool
}
