# P1 子 Agent 执行提示词：apiserver 同包符号聚合

## 角色

你是 `P1 Agent`，负责 `codex-isolation` 工作流 Step 1 / P1 阶段。你的上游（P0 + Pre-P1）已由主 Agent 完成，交接状态见 `.agent/workflows/codex-isolation-handoffs/LATEST.md`。

## 目标

在 `internal/apiserver` 包**内部**，将 codex 专属符号从大文件中拆分到独立文件簇（`*_codex*.go`），不跨包、不改行为、不改方法签名、不改 JSON-RPC method name。目的是降低 P3 跨包抽取时的冲突面。

## 约束

1. **同包重排，不跨包** — 所有改文件都在 `internal/apiserver/` 内
2. **行为零变更** — 不改函数签名、不改返回值语义、不改 JSON-RPC method name
3. **测试基线必须通过** — Pre-P1 已建立 ~30 个纯函数测试作为重构护栏
4. **仅改允许路径**:
   ```
   internal/apiserver/methods_thread.go     (拆出 codex 部分)
   internal/apiserver/methods_helpers.go     (拆出 codex 部分)
   internal/apiserver/methods_turn.go        (拆出 codex 部分)
   internal/apiserver/turn_tracker.go        (拆出 codex 部分)
   internal/apiserver/*_codex*.go            (新增)
   internal/apiserver/*_skills*.go           (新增，可选)
   ```

## 新增文件建议

| 新文件 | 从哪里拆 | 放什么 |
|---|---|---|
| `methods_thread_codex.go` | `methods_thread.go` | codex 专属: `threadResumeTyped`, `threadMessagesTyped`, rollout 相关(`resolveRolloutHistorySource`, `loadAllThreadMessagesFromCodexRollout`, `parseRolloutTimestamp`, `paginateRolloutMessages`, `streamRemainingHistory`, `msgsToRecords`), archive 全族(`threadArchiveTyped`, `threadUnarchiveTyped`, `archiveThreadArtifacts`, `collectThreadArtifactCandidates`, `pruneArchivedCodexSourceFiles`, `restoreThreadArchiveSources`, `inspectThreadArchiveForRestore`, `findLatestThreadArchiveManifestPath`, `readThreadArchiveManifest`, `writeThreadArchiveManifest`, ...), `threadNameSetTyped`, `threadRollbackTyped` |
| `methods_helpers_codex.go` | `methods_helpers.go` | codex 专属: `isLikelyCodexThreadID`, `normalizeCodexThreadID`, `appendUniqueThreadID`, `buildResumeCandidates`, `tryResumeCandidates`, `previewResumeCandidates`, `isHistoricalResumeCandidateError`, `isCodexProcessCrashError`, `buildSessionLostNotification`, `resolvePrimaryCodexThreadID`, `resolveCodexThreadCandidates`, `ensureThreadReadyForTurn`, `registerBinding`, `threadExistsInHistory`, `resolveSlashCommandThread`, `resolveThreadForSlashCommand`, `sendSlashCommand`, `sendSlashCommandWithArgs` |
| `methods_turn_codex.go` | `methods_turn.go` | codex 专属: `activeTurnIDReader`(interface), `resolveClientActiveTurnID`, `turnStartTyped`, `turnSteerTyped`, `turnInterrupt`, `turnForceComplete`, `reviewStartTyped`, `normalizeInterruptState`, `isInterruptActiveState`, `isInterruptNoActiveTurnError`, `readThreadRuntimeState`, `waitInterruptSettled`, `waitInterruptOutcome`, `interruptSettleMode` |
| `turn_tracker_codex.go` | `turn_tracker.go` | codex turn 跟踪核心: `trackedTurn` struct, `beginTrackedTurn`, `hasActiveTrackedTurn`, `markTrackedTurnInterruptRequested`, `waitTrackedTurnTerminal`, `completeTrackedTurn`, `completeTrackedTurnByID`, `peekTrackedTurnMeta`, `maybeFinalizeTrackedTurn`, stall 检测全族(`checkTurnStall`, `rescheduleStallCheck`, `handleStallGracePeriod`, `executeStallAutoInterrupt`, `touchTrackedTurnLastEvent`, `markTrackedTurnStallHint`, `shouldLogTrackedTurnStallHint`) |

## 保留在原文件的内容（通用侧）

- **`methods_thread.go` 保留**: 线程 CRUD/list/read DTO (`threadListItem`, `threadListResponse` 等), `threadList`, `threadLoadedList`, `threadReadTyped`, `threadResolveTyped`, `buildThreadSnapshots*`, `append*Threads`, `appendThreadHistoryFromStores`, 通用纯函数(`calculateHydrationLoadLimit`, `sanitizeArchiveName*`, `inferThreadArtifactKind`, `pathWithinRoot`, `normalizeThreadArchiveMap`, `copyFile*`, `fileSHA256`, 各种 resolve/archive 目录函数)
- **`methods_helpers.go` 保留**: `withThread`, `extractInputs`, `buildAttachmentName`, `buildAttachmentPreviewURL`, `buildUserTimelineAttachments*`, slash command handlers (`threadBgTerminalsClean`, `threadUndo`, `threadModelSet`, `threadPersonality`, `threadApprovals`, `threadMCPList`, `threadSkillsList`, `threadDebugMemory`), debug 运行时 (`debugRuntime`, `debugForceGC`)
- **`methods_turn.go` 保留**: DTO (`UserInput`, `turnStartParams`, `turnInfo`, 等), skill prompt 组装全族 (`collectInputSkillNames`, `collectSkillNameSet`, `mergePromptText`, `skillInputText`, `fileContentInputText`, `composeUserTimelineTextForTurn`, `validateLSPUsagePromptHint`, `collectReferencedLSPToolNames`, `prependLSPAvailabilityWarning`, `classifyAutoSkillMatch`, `explicitSkillMentionTerms`, `lowerMatchedTerms`, `forceMatchedSkillInstruction`, …), fuzzy 搜索 (`fuzzyMatch`, `fuzzyFileSearchTyped`), skill 规范化 (`normalizeSkillName`, `normalizeSkillNames`)
- **`turn_tracker.go` 保留**: Payload 解析纯函数 (`trackedTurnSummaryFromPayload`, `normalizeTrackedTurnStatus`, `threadStatusTerminalFromPayload`, `mergeTrackedTurnCompletionPayload`, `injectTrackedTurnSummary`, `extractTrackedTurnID/Status/Reason`, `extractTrackedString`, `trackedTurnSummaryCacheKey`, `trackedTurnPayloadDiagKV`, `trackedTurnTerminalFromEvent`, `extractTrackedRetryable`), 摘要缓存 (`rememberTrackedTurnSummary`, `lookupTrackedTurnSummary`, `pruneTrackedTurnSummaryCacheLocked`, `captureAndInjectTurnSummary`)

## 执行步骤

### 批次 1: `methods_turn_codex.go`（风险最低，函数独立性高）

1. 在 `internal/apiserver/` 新建 `methods_turn_codex.go`
2. 将 codex 侧符号从 `methods_turn.go` 剪切过去（保留 import 和 package 声明）
3. 运行 `go build ./internal/apiserver/...` 确认编译通过
4. 运行 `go test ./internal/apiserver/... -count=1` 确认测试通过

### 批次 2: `methods_helpers_codex.go`

1. 新建 `methods_helpers_codex.go`
2. 将 codex 侧符号从 `methods_helpers.go` 剪切过去
3. 编译 + 测试

### 批次 3: `methods_thread_codex.go`（最大文件，最多符号）

1. 新建 `methods_thread_codex.go`
2. 将 codex 侧符号（含全部 archive 族、rollout 族、resume 族）从 `methods_thread.go` 剪切过去
3. 编译 + 测试

### 批次 4: `turn_tracker_codex.go`

1. 新建 `turn_tracker_codex.go`
2. 将 codex turn 跟踪 + stall 检测从 `turn_tracker.go` 剪切过去
3. 编译 + 测试

### 批次 5: 最终验证

```bash
go build ./...
go test ./internal/apiserver/... -count=1
go vet ./internal/apiserver/...
# 验证 codex 符号分布
rg -n "Client\.Submit|Client\.SendCommand|Client\.GetThreadID|Client\.ResumeThread" internal/apiserver | sort
# 确认新文件存在
ls -la internal/apiserver/*_codex*.go
```

## 完成标准

1. `go build ./...` ✅
2. `go test ./internal/apiserver/... -count=1` ✅（Pre-P1 测试必须全部通过）
3. `go vet ./internal/apiserver/...` ✅
4. 新文件 `*_codex*.go` 存在且包含 codex 专属符号
5. 原文件仅保留通用侧符号
6. 无行为变更、无签名变更

## 交接

完成后更新:
- `.agent/workflows/codex-isolation-handoffs/LATEST.md` → `current_phase: P1, status: done, next_phase: P2`
- 生成 `p1.md` 交接报告（列出新增文件、迁移符号清单、验证日志）
- 生成 `p1.checks.log`（验证命令输出）
- 生成 `p1.files.txt`（本阶段改动文件清单）

## 关键提醒

- **每批次之间必须编译+测试**，失败立即停止并报告阻塞点
- 如果某个函数依赖其他函数（例如 `ensureThreadReadyForTurn` 调用 `isLikelyCodexThreadID`），两者必须一起迁移或保持可见性（同包无问题）
- 不要移动测试文件（`*_test.go`），测试文件保持原位
- 移动时注意 import：新文件可能需要不同的 import 集合，原文件可能需要删除不再使用的 import
