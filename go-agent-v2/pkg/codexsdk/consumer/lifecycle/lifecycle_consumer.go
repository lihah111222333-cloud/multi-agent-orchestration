package lifecycle

import (
	"context"
	"encoding/json"

	"github.com/multi-agent/go-agent-v2/pkg/codexsdk"
	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/common"
	lifecyclesvc "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/lifecycle"
)

type AgentInfo = lifecyclesvc.AgentInfo
type ThreadSnapshot = lifecyclesvc.ThreadSnapshot
type ThreadStartResult = lifecyclesvc.ThreadStartResult
type ThreadResumeResult = lifecyclesvc.ThreadResumeResult
type ThreadForkResult = lifecyclesvc.ThreadForkResult

func AppendUniqueThreadIDFallback(dst []string, seen map[string]struct{}, candidate string) []string {
	return common.AppendUniqueThreadIDFallback(dst, seen, candidate)
}

func RunThreadStart(
	ctx context.Context,
	threadID string,
	cwd string,
	model string,
	modelProvider string,
	approvalPolicy string,
	dynamicTools []codexsdk.DynamicTool,
	launchThread func(context.Context, string, string, string, string, string, []codexsdk.DynamicTool) error,
	getProcess func(string) any,
	listAgents func() []AgentInfo,
	resolveStartInstructions func(context.Context, []codexsdk.DynamicTool) string,
	registerBinding func(context.Context, string, any),
	syncRuntimeThreads func([]AgentInfo),
) (ThreadStartResult, error) {
	return lifecyclesvc.RunThreadStart(
		ctx,
		threadID,
		cwd,
		model,
		modelProvider,
		approvalPolicy,
		dynamicTools,
		launchThread,
		getProcess,
		listAgents,
		resolveStartInstructions,
		registerBinding,
		syncRuntimeThreads,
	)
}

func RunThreadResume(
	ctx context.Context,
	threadID string,
	path string,
	cwd string,
	model string,
	proc any,
	resolveCandidates func(context.Context, string, func([]string, map[string]struct{}, string) []string, func([]string, int) []string) []string,
	normalizeThreadID func(string) string,
	resumeThread func(any, codexsdk.ResumeThreadRequest) error,
) (ThreadResumeResult, error) {
	return lifecyclesvc.RunThreadResume(
		ctx,
		threadID,
		path,
		cwd,
		model,
		proc,
		resolveCandidates,
		normalizeThreadID,
		resumeThread,
	)
}

func RunThreadFork(
	threadID string,
	proc any,
	forkThread func(any, codexsdk.ForkThreadRequest) (*codexsdk.ForkThreadResponse, error),
	nowUnixMilli func() int64,
) (ThreadForkResult, error) {
	return lifecyclesvc.RunThreadFork(threadID, proc, forkThread, nowUnixMilli)
}

func RunThreadRealtimeStart(threadID, prompt string) (map[string]any, error) {
	return lifecyclesvc.RunThreadRealtimeStart(threadID, prompt)
}

func RunThreadRealtimeAppendAudio(threadID string, audio any) (map[string]any, error) {
	return lifecyclesvc.RunThreadRealtimeAppendAudio(threadID, audio)
}

func RunThreadRealtimeAppendText(threadID, text string) (map[string]any, error) {
	return lifecyclesvc.RunThreadRealtimeAppendText(threadID, text)
}

func RunThreadRealtimeStop(threadID string) (map[string]any, error) {
	return lifecyclesvc.RunThreadRealtimeStop(threadID)
}

func RunTurnSteer(
	proc any,
	submit func(any, string, []string, []string, json.RawMessage) error,
	submitPrompt string,
	images []string,
	files []string,
) (map[string]any, error) {
	return lifecyclesvc.RunTurnSteer(proc, submit, submitPrompt, images, files)
}

func RunThreadCommand(
	proc any,
	methodName string,
	command string,
	args string,
	wrapMsg string,
	sendCommand func(any, string, string) error,
) (map[string]any, error) {
	return lifecyclesvc.RunThreadCommand(proc, methodName, command, args, wrapMsg, sendCommand)
}

func RunThreadNameSet(
	ctx context.Context,
	threadID string,
	name string,
	getProcess func(string) any,
	threadExistsInRuntime func(string) bool,
	threadExistsInHistory func(context.Context, string) bool,
	sendCommand func(any, string, string) error,
	setRuntimeThreadName func(string, string),
	persistThreadAlias func(context.Context, string, string) error,
) (map[string]any, error) {
	return lifecyclesvc.RunThreadNameSet(
		ctx,
		threadID,
		name,
		getProcess,
		threadExistsInRuntime,
		threadExistsInHistory,
		sendCommand,
		setRuntimeThreadName,
		persistThreadAlias,
	)
}

func RunThreadRead(proc any, listThreads func(any) ([]codexsdk.ThreadInfo, error)) (map[string]any, error) {
	return lifecyclesvc.RunThreadRead(proc, listThreads)
}

func RunThreadResolve(
	ctx context.Context,
	threadID string,
	resolveRunningThreadIdentity func(string) (state string, port int, codexThreadID string, found bool),
	firstResolvedCodexThreadID func(context.Context, string) string,
	threadExistsInHistory func(context.Context, string) bool,
) (map[string]any, error) {
	return lifecyclesvc.RunThreadResolve(
		ctx,
		threadID,
		resolveRunningThreadIdentity,
		firstResolvedCodexThreadID,
		threadExistsInHistory,
	)
}

func FirstResolvedCodexThreadIDFromCandidates(
	ctx context.Context,
	threadID string,
	resolveCandidates func(context.Context, string, func([]string, map[string]struct{}, string) []string, func([]string, int) []string) []string,
) string {
	return lifecyclesvc.FirstResolvedCodexThreadIDFromCandidates(ctx, threadID, resolveCandidates)
}

func ResolveRunningThreadIdentityFromAgents(threadID string, agents []AgentInfo) (state string, port int, codexThreadID string, found bool) {
	return lifecyclesvc.ResolveRunningThreadIdentityFromAgents(threadID, agents)
}

func ThreadExistsInRuntimeSnapshots(threadID string, snapshots []ThreadSnapshot) bool {
	return lifecyclesvc.ThreadExistsInRuntimeSnapshots(threadID, snapshots)
}

func NormalizeCodexThreadID(raw string) string {
	return lifecyclesvc.NormalizeCodexThreadID(raw)
}

func IsLikelyCodexThreadID(raw string) bool {
	return lifecyclesvc.IsLikelyCodexThreadID(raw)
}

func BuildResumeCandidates(threadID string, resolved []string, normalize func(string) string) []string {
	return lifecyclesvc.BuildResumeCandidates(threadID, resolved, normalize)
}

func TryResumeCandidates(
	candidates []string,
	fallbackID string,
	resumeFn func(string) error,
	isCandidateError func(error) bool,
) (string, error) {
	return lifecyclesvc.TryResumeCandidates(candidates, fallbackID, resumeFn, isCandidateError)
}

func IsHistoricalResumeCandidateError(err error) bool {
	return lifecyclesvc.IsHistoricalResumeCandidateError(err)
}

func IsCodexProcessCrashError(err error) bool {
	return lifecyclesvc.IsCodexProcessCrashError(err)
}

func PreviewResumeCandidates(candidates []string, limit int) []string {
	return lifecyclesvc.PreviewResumeCandidates(candidates, limit)
}
