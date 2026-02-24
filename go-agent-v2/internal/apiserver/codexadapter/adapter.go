package codexadapter

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/multi-agent/go-agent-v2/internal/agentcore"
	"github.com/multi-agent/go-agent-v2/internal/runner"
	"github.com/multi-agent/go-agent-v2/internal/store"
	"github.com/multi-agent/go-agent-v2/internal/uistate"
)

// ServerContext 暴露 codex 适配层所需的服务端能力边界。
// 通过接口隔离 apiserver 内部实现，避免反向依赖具体结构体字段。
type ServerContext interface {
	Manager() *runner.AgentManager
	Store() *uistate.PreferenceManager
	BindingStore() *store.AgentCodexBindingStore
	AgentStatusStore() *store.AgentStatusStore
	UIRuntime() *uistate.RuntimeManager
	Notify(method string, params any)
}

// Adapter 封装对 proc.Client 的直接访问。
type Adapter struct {
	ctx ServerContext
}

// New 创建 codex 适配器。
func New(ctx ServerContext) *Adapter {
	return &Adapter{ctx: ctx}
}

// Context 返回当前绑定的服务端上下文。
func (a *Adapter) Context() ServerContext {
	if a == nil {
		return nil
	}
	return a.ctx
}

// Submit 发送用户输入到 codex。
func (a *Adapter) Submit(proc *runner.AgentProcess, prompt string, images, files []string, outputSchema json.RawMessage) error {
	if proc == nil || proc.Client == nil {
		return errors.New("codexadapter: agent process not found")
	}
	return proc.Client.Submit(prompt, images, files, outputSchema)
}

// SendCommand 发送 slash 命令到 codex。
func (a *Adapter) SendCommand(proc *runner.AgentProcess, command string, args string) error {
	if proc == nil || proc.Client == nil {
		return errors.New("codexadapter: agent process not found")
	}
	return proc.Client.SendCommand(command, args)
}

// GetThreadID 读取当前 codex thread id。
func (a *Adapter) GetThreadID(proc *runner.AgentProcess) string {
	if proc == nil || proc.Client == nil {
		return ""
	}
	return strings.TrimSpace(proc.Client.GetThreadID())
}

// ResumeThread 恢复历史 codex thread。
func (a *Adapter) ResumeThread(proc *runner.AgentProcess, req agentcore.ResumeThreadRequest) error {
	if proc == nil || proc.Client == nil {
		return errors.New("codexadapter: agent process not found")
	}
	return proc.Client.ResumeThread(req)
}

// ListThreads 查询 codex 线程列表。
func (a *Adapter) ListThreads(proc *runner.AgentProcess) ([]agentcore.ThreadInfo, error) {
	if proc == nil || proc.Client == nil {
		return nil, errors.New("codexadapter: agent process not found")
	}
	return proc.Client.ListThreads()
}

// ForkThread 基于指定源线程创建分叉线程。
func (a *Adapter) ForkThread(proc *runner.AgentProcess, req agentcore.ForkThreadRequest) (*agentcore.ForkThreadResponse, error) {
	if proc == nil || proc.Client == nil {
		return nil, errors.New("codexadapter: agent process not found")
	}
	return proc.Client.ForkThread(req)
}

// RespondError 回传 dynamic tool 调用错误。
func (a *Adapter) RespondError(proc *runner.AgentProcess, id int64, code int, message string) error {
	if proc == nil || proc.Client == nil {
		return errors.New("codexadapter: agent process not found")
	}
	return proc.Client.RespondError(id, code, message)
}

// SendDynamicToolResult 回传 dynamic tool 调用结果。
func (a *Adapter) SendDynamicToolResult(proc *runner.AgentProcess, callID, output string, requestID *int64) error {
	if proc == nil || proc.Client == nil {
		return errors.New("codexadapter: agent process not found")
	}
	return proc.Client.SendDynamicToolResult(callID, output, requestID)
}

// CollectThreadArtifactCandidates proxies package-level archive candidate discovery.
func (a *Adapter) CollectThreadArtifactCandidates(codexThreadID string, rolloutPath string) []ThreadArtifactCandidate {
	return CollectThreadArtifactCandidates(codexThreadID, rolloutPath)
}

// PruneArchivedCodexSourceFiles proxies package-level source pruning.
func (a *Adapter) PruneArchivedCodexSourceFiles(opt PruneArchivedCodexSourceFilesOptions) {
	PruneArchivedCodexSourceFiles(opt)
}

// RestoreThreadArchiveSources proxies package-level source restore.
func (a *Adapter) RestoreThreadArchiveSources(opt RestoreThreadArchiveSourcesOptions) ([]string, []string, error) {
	return RestoreThreadArchiveSources(opt)
}

// InspectThreadArchiveForRestore proxies package-level archive integrity inspection.
func (a *Adapter) InspectThreadArchiveForRestore(opt InspectThreadArchiveForRestoreOptions) (ThreadArchiveRestoreNotice, error) {
	return InspectThreadArchiveForRestore(opt)
}

// FindLatestThreadArchiveManifestPath proxies package-level latest-manifest resolution.
func (a *Adapter) FindLatestThreadArchiveManifestPath(threadDir string) (string, error) {
	return FindLatestThreadArchiveManifestPath(threadDir)
}

// ReadThreadArchiveManifest proxies package-level manifest read.
func (a *Adapter) ReadThreadArchiveManifest(manifestPath string) (ThreadArchiveManifest, error) {
	return ReadThreadArchiveManifest(manifestPath)
}

// WriteThreadArchiveManifest proxies package-level manifest write.
func (a *Adapter) WriteThreadArchiveManifest(manifest ThreadArchiveManifest) error {
	return WriteThreadArchiveManifest(manifest)
}
