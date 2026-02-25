package codexadapter

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

const prefThreadArchivesChat = "threadArchives.chat"

type ThreadHistoryMessage struct {
	ID        int64           `json:"id"`
	AgentID   string          `json:"agentId"`
	Role      string          `json:"role"`
	EventType string          `json:"eventType"`
	Method    string          `json:"method"`
	Content   string          `json:"content"`
	Metadata  json.RawMessage `json:"metadata,omitempty"`
	CreatedAt time.Time       `json:"createdAt"`
}

type ThreadMessagesOptions struct {
	ThreadID string
	Limit    int
	Before   int64

	LoadAllMessages func(context.Context, string) ([]ThreadHistoryMessage, error)
	Paginate        func([]ThreadHistoryMessage, int, int64) []ThreadHistoryMessage
	HandleHydration func(threadID string, all []ThreadHistoryMessage, page []ThreadHistoryMessage, before int64, limit int)
	Stats           func(threadID string) (diffLen int, timelineLen int)
}

// ThreadMessagesByID loads and paginates thread messages using constructor-time dependencies.
func (a *Adapter) ThreadMessagesByID(ctx context.Context, threadID string, limit int, before int64) (map[string]any, error) {
	return a.ThreadMessages(ctx, ThreadMessagesOptions{
		ThreadID:        threadID,
		Limit:           limit,
		Before:          before,
		LoadAllMessages: a.LoadAllThreadMessagesFromRollout,
		Paginate:        PaginateRolloutMessages,
		HandleHydration: a.handleThreadMessagesHydration,
		Stats:           a.threadMessagesStats,
	})
}

// ThreadMessages 负责消息分页主流程；具体 hydration 细节由回调实现。
func (a *Adapter) ThreadMessages(ctx context.Context, opt ThreadMessagesOptions) (map[string]any, error) {
	if strings.TrimSpace(opt.ThreadID) == "" {
		return nil, apperrors.New("Server.threadMessages", "threadId is required")
	}
	if opt.LoadAllMessages == nil {
		return nil, apperrors.New("Server.threadMessages", "message loader is not configured")
	}
	paginate := opt.Paginate
	if paginate == nil {
		paginate = PaginateRolloutMessages
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	allMsgs, err := opt.LoadAllMessages(ctx, opt.ThreadID)
	if err != nil {
		return nil, apperrors.Wrap(err, "Server.threadMessages", "load codex rollout messages")
	}
	total := int64(len(allMsgs))
	msgs := paginate(allMsgs, opt.Limit, opt.Before)
	logger.Info("thread/messages: page selected",
		logger.FieldAgentID, opt.ThreadID, logger.FieldThreadID, opt.ThreadID,
		"before", opt.Before,
		"limit", opt.Limit,
		"page_count", len(msgs),
		"total", total,
	)

	if opt.HandleHydration != nil {
		opt.HandleHydration(opt.ThreadID, allMsgs, msgs, opt.Before, opt.Limit)
	}

	diffLen := 0
	timelineLen := 0
	if opt.Stats != nil {
		diffLen, timelineLen = opt.Stats(opt.ThreadID)
	}
	logger.Info("thread/messages: response prepared",
		logger.FieldAgentID, opt.ThreadID, logger.FieldThreadID, opt.ThreadID,
		"page_count", len(msgs),
		"total", total,
		"timeline_len", timelineLen,
		"diff_len", diffLen,
	)
	return map[string]any{
		"messages": msgs,
		"total":    total,
	}, nil
}

// ThreadArchive validates archive eligibility, archives artifacts, and persists archive state.
func (a *Adapter) ThreadArchive(ctx context.Context, threadID string) (map[string]any, error) {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil, apperrors.New("Server.threadArchive", "threadId is required")
	}
	if !a.ThreadExistsForArchiveByID(ctx, threadID) {
		return nil, apperrors.Newf("Server.threadArchive", "thread %s not found", threadID)
	}
	manifest, err := a.archiveThreadArtifacts(ctx, threadID)
	if err != nil {
		return nil, apperrors.Wrap(err, "Server.threadArchive", "archive codex artifacts")
	}
	archivedAt := time.Now().UnixMilli()
	if err := a.PersistThreadArchivedStateByID(ctx, threadID, archivedAt); err != nil {
		return nil, apperrors.Wrap(err, "Server.threadArchive", "persist archive state")
	}

	return map[string]any{
		"ok":            true,
		"threadId":      threadID,
		"archivedAt":    archivedAt,
		"codexThreadId": manifest.CodexThreadID,
		"archiveDir":    manifest.ArchiveDir,
		"rolloutPath":   manifest.RolloutPath,
		"files":         manifest.Files,
	}, nil
}

func (a *Adapter) threadExistsInHistory(ctx context.Context, threadID string) bool {
	return a.ThreadExistsInHistory(ctx, threadID)
}

func (a *Adapter) loadThreadArchiveMap(ctx context.Context) (map[string]int64, error) {
	if a == nil || a.ctx == nil || a.ctx.Store() == nil {
		return map[string]int64{}, nil
	}
	value, err := a.ctx.Store().Get(ctx, prefThreadArchivesChat)
	if err != nil {
		return nil, err
	}
	return NormalizeThreadArchiveMap(value), nil
}

func (a *Adapter) archiveThreadArtifacts(ctx context.Context, threadID string) (ThreadArchiveManifest, error) {
	return a.ArchiveThreadArtifacts(ctx, ArchiveThreadArtifactsOptions{
		ThreadID:                        threadID,
		ResolveThreadArchiveRootDir:     resolveThreadArchiveRootDir,
		ResolveThreadArchiveSnapshotDir: resolveThreadArchiveSnapshotDir,
		ResolveRolloutHistorySource: func(resolveCtx context.Context, id string) (string, string) {
			return a.ResolveRolloutHistorySource(resolveCtx, id, a.normalizeCodexThreadID)
		},
		NormalizeCodexThreadID:          a.normalizeCodexThreadID,
		CollectThreadArtifactCandidates: CollectThreadArtifactCandidates,
		NextArchiveFilePath:             nextArchiveFilePath,
		CopyFile:                        copyFile,
		FileSHA256:                      fileSHA256,
		WriteThreadArchiveManifest:      WriteThreadArchiveManifest,
		BindRolloutPath: func(bindCtx context.Context, agentID, codexThreadID, rolloutPath string) error {
			if a == nil || a.ctx == nil || a.ctx.BindingStore() == nil {
				return nil
			}
			dbCtx, cancel := context.WithTimeout(bindCtx, 5*time.Second)
			defer cancel()
			return a.ctx.BindingStore().Bind(dbCtx, agentID, codexThreadID, rolloutPath)
		},
		PruneArchivedCodexSourceFiles: func(id string, files []ThreadArchiveFile, archiveDir string) {
			PruneArchivedCodexSourceFiles(PruneArchivedCodexSourceFilesOptions{
				ThreadID:                  id,
				Files:                     files,
				ArchiveDir:                archiveDir,
				ResolveCodexRootDir:       resolveCodexRootDir,
				PathWithinRoot:            PathWithinRoot,
				FileSHA256:                fileSHA256,
				RemoveEmptyCodexParentDir: removeEmptyCodexParentDirs,
			})
		},
	})
}

// ArchiveThreadArtifactsByID archives thread artifacts using constructor-time dependencies.
func (a *Adapter) ArchiveThreadArtifactsByID(ctx context.Context, threadID string) (ThreadArchiveManifest, error) {
	return a.archiveThreadArtifacts(ctx, threadID)
}

// ThreadExistsForArchive checks whether a thread can be archived.
func (a *Adapter) ThreadExistsForArchive(ctx context.Context, threadID string, threadExistsInHistory func(context.Context, string) bool) bool {
	id := strings.TrimSpace(threadID)
	if id == "" {
		return false
	}
	if a != nil && a.ctx != nil {
		if mgr := a.ctx.Manager(); mgr != nil && mgr.Get(id) != nil {
			return true
		}
		if runtime := a.ctx.UIRuntime(); runtime != nil {
			for _, item := range runtime.SnapshotLight().Threads {
				if strings.TrimSpace(item.ID) == id {
					return true
				}
			}
		}
	}
	return threadExistsInHistory != nil && threadExistsInHistory(ctx, id)
}

// ThreadExistsForArchiveByID checks whether a thread can be archived using constructor-time dependencies.
func (a *Adapter) ThreadExistsForArchiveByID(ctx context.Context, threadID string) bool {
	return a.ThreadExistsForArchive(ctx, threadID, a.threadExistsInHistory)
}

// PersistThreadArchivedState writes thread archive marker to preference storage.
func (a *Adapter) PersistThreadArchivedState(
	ctx context.Context,
	threadID string,
	archivedAt int64,
	loadThreadArchiveMap func(context.Context) (map[string]int64, error),
) error {
	if a == nil || a.ctx == nil || a.ctx.Store() == nil {
		return nil
	}
	id := strings.TrimSpace(threadID)
	if id == "" {
		return nil
	}
	if loadThreadArchiveMap == nil {
		return apperrors.New("persistThreadArchivedState", "thread archive map loader is not configured")
	}
	archivedMap, err := loadThreadArchiveMap(ctx)
	if err != nil {
		return err
	}
	if archivedMap == nil {
		archivedMap = map[string]int64{}
	}
	if archivedAt <= 0 {
		archivedAt = time.Now().UnixMilli()
	}
	archivedMap[id] = archivedAt
	return a.ctx.Store().Set(ctx, prefThreadArchivesChat, archivedMap)
}

// PersistThreadArchivedStateByID writes archive marker using constructor-time dependencies.
func (a *Adapter) PersistThreadArchivedStateByID(ctx context.Context, threadID string, archivedAt int64) error {
	return a.PersistThreadArchivedState(ctx, threadID, archivedAt, a.loadThreadArchiveMap)
}

// RemoveThreadArchivedState clears thread archive marker from preference storage.
func (a *Adapter) RemoveThreadArchivedState(
	ctx context.Context,
	threadID string,
	loadThreadArchiveMap func(context.Context) (map[string]int64, error),
) error {
	if a == nil || a.ctx == nil || a.ctx.Store() == nil {
		return nil
	}
	id := strings.TrimSpace(threadID)
	if id == "" {
		return nil
	}
	if loadThreadArchiveMap == nil {
		return apperrors.New("removeThreadArchivedState", "thread archive map loader is not configured")
	}
	archivedMap, err := loadThreadArchiveMap(ctx)
	if err != nil {
		return err
	}
	if archivedMap == nil {
		archivedMap = map[string]int64{}
	}
	delete(archivedMap, id)
	return a.ctx.Store().Set(ctx, prefThreadArchivesChat, archivedMap)
}

// RemoveThreadArchivedStateByID clears archive marker using constructor-time dependencies.
func (a *Adapter) RemoveThreadArchivedStateByID(ctx context.Context, threadID string) error {
	return a.RemoveThreadArchivedState(ctx, threadID, a.loadThreadArchiveMap)
}

type ThreadArchiveFile struct {
	Kind         string `json:"kind"`
	SourcePath   string `json:"sourcePath"`
	ArchivedPath string `json:"archivedPath"`
	SizeBytes    int64  `json:"sizeBytes"`
	SHA256       string `json:"sha256,omitempty"`
}

type ThreadArchiveManifest struct {
	ThreadID      string              `json:"threadId"`
	CodexThreadID string              `json:"codexThreadId,omitempty"`
	ArchivedAt    string              `json:"archivedAt"`
	ArchiveDir    string              `json:"archiveDir"`
	RolloutPath   string              `json:"rolloutPath,omitempty"`
	Files         []ThreadArchiveFile `json:"files"`
}

type ThreadArtifactCandidate struct {
	Kind string
	Path string
}

type ArchiveThreadArtifactsOptions struct {
	ThreadID string

	ResolveThreadArchiveRootDir     func() (string, error)
	ResolveThreadArchiveSnapshotDir func(rootDir string, threadID string, archivedAt string) (string, error)
	ResolveRolloutHistorySource     func(context.Context, string) (string, string)
	NormalizeCodexThreadID          func(string) string
	CollectThreadArtifactCandidates func(codexThreadID string, rolloutPath string) []ThreadArtifactCandidate
	NextArchiveFilePath             func(dir, filename string) (string, error)
	CopyFile                        func(srcPath, targetPath string) error
	FileSHA256                      func(path string) (string, error)
	WriteThreadArchiveManifest      func(manifest ThreadArchiveManifest) error
	BindRolloutPath                 func(context.Context, string, string, string) error
	PruneArchivedCodexSourceFiles   func(threadID string, files []ThreadArchiveFile, archiveDir string)
}

// ArchiveThreadArtifacts 归档 codex 线程相关文件。
func (a *Adapter) ArchiveThreadArtifacts(ctx context.Context, opt ArchiveThreadArtifactsOptions) (ThreadArchiveManifest, error) {
	id := strings.TrimSpace(opt.ThreadID)
	manifest := ThreadArchiveManifest{
		ThreadID:   id,
		ArchivedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Files:      []ThreadArchiveFile{},
	}
	rootDir, err := opt.ResolveThreadArchiveRootDir()
	if err != nil {
		return manifest, apperrors.Wrap(err, "Server.archiveThreadArtifacts", "resolve archive root")
	}
	archiveDir, err := opt.ResolveThreadArchiveSnapshotDir(rootDir, id, manifest.ArchivedAt)
	if err != nil {
		return manifest, apperrors.Wrap(err, "Server.archiveThreadArtifacts", "resolve archive dir")
	}
	manifest.ArchiveDir = archiveDir
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		return manifest, apperrors.Wrap(err, "Server.archiveThreadArtifacts", "ensure archive dir")
	}

	codexThreadID, rolloutPath := opt.ResolveRolloutHistorySource(ctx, id)
	if opt.NormalizeCodexThreadID != nil {
		manifest.CodexThreadID = opt.NormalizeCodexThreadID(codexThreadID)
	} else {
		manifest.CodexThreadID = strings.TrimSpace(codexThreadID)
	}
	candidates := opt.CollectThreadArtifactCandidates(manifest.CodexThreadID, rolloutPath)

	for _, candidate := range candidates {
		srcPath := strings.TrimSpace(candidate.Path)
		if srcPath == "" {
			continue
		}
		info, err := os.Stat(srcPath)
		if err != nil || info.IsDir() {
			continue
		}
		targetPath, err := opt.NextArchiveFilePath(archiveDir, filepath.Base(srcPath))
		if err != nil {
			return manifest, apperrors.Wrap(err, "Server.archiveThreadArtifacts", "resolve archive target")
		}
		if err := opt.CopyFile(srcPath, targetPath); err != nil {
			logger.Error("thread/archive: copy artifact failed",
				logger.FieldThreadID, id,
				"source_path", srcPath,
				"target_path", targetPath,
				logger.FieldError, err,
			)
			continue
		}
		fileMeta := ThreadArchiveFile{
			Kind:         candidate.Kind,
			SourcePath:   srcPath,
			ArchivedPath: targetPath,
			SizeBytes:    info.Size(),
		}
		checksum, err := opt.FileSHA256(targetPath)
		if err != nil {
			return manifest, apperrors.Wrap(err, "Server.archiveThreadArtifacts", "compute archived file checksum")
		}
		fileMeta.SHA256 = checksum
		manifest.Files = append(manifest.Files, fileMeta)
		if manifest.RolloutPath == "" && candidate.Kind == "rollout" {
			manifest.RolloutPath = targetPath
		}
	}
	sort.SliceStable(manifest.Files, func(i, j int) bool {
		return manifest.Files[i].ArchivedPath < manifest.Files[j].ArchivedPath
	})

	if err := opt.WriteThreadArchiveManifest(manifest); err != nil {
		return manifest, apperrors.Wrap(err, "Server.archiveThreadArtifacts", "write manifest")
	}
	if opt.BindRolloutPath != nil && manifest.CodexThreadID != "" && strings.TrimSpace(manifest.RolloutPath) != "" {
		if err := opt.BindRolloutPath(ctx, id, manifest.CodexThreadID, manifest.RolloutPath); err != nil {
			logger.Warn("thread/archive: persist rollout path failed",
				logger.FieldThreadID, id,
				"codex_thread_id", manifest.CodexThreadID,
				"rollout_path", manifest.RolloutPath,
				logger.FieldError, err,
			)
		}
	}
	if opt.PruneArchivedCodexSourceFiles != nil {
		opt.PruneArchivedCodexSourceFiles(id, manifest.Files, manifest.ArchiveDir)
	}
	return manifest, nil
}
