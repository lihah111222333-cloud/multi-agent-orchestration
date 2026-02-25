package codexadapter

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

func (a *Adapter) bindRolloutPath(ctx context.Context, agentID, codexThreadID, rolloutPath string) {
	if strings.TrimSpace(codexThreadID) == "" || strings.TrimSpace(rolloutPath) == "" {
		return
	}
	if a == nil || a.ctx == nil || a.ctx.BindingStore == nil {
		return
	}
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := a.ctx.BindingStore.Bind(dbCtx, agentID, codexThreadID, rolloutPath); err != nil {
		logger.Warn("thread/archive: persist rollout path failed",
			logger.FieldThreadID, agentID,
			"codex_thread_id", codexThreadID,
			"rollout_path", rolloutPath,
			logger.FieldError, err,
		)
	}
}

func (a *Adapter) pruneArchivedCodexSourceFiles(threadID string, files []ThreadArchiveFile, archiveDir string) {
	PruneArchivedCodexSourceFiles(
		threadID,
		files,
		archiveDir,
		resolveCodexRootDir,
		PathWithinRoot,
		fileSHA256,
		removeEmptyCodexParentDirs,
	)
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

// ArchiveThreadArtifacts 归档 codex 线程相关文件。
func (a *Adapter) ArchiveThreadArtifacts(ctx context.Context, threadID string) (ThreadArchiveManifest, error) {
	id := strings.TrimSpace(threadID)
	manifest := ThreadArchiveManifest{
		ThreadID:   id,
		ArchivedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Files:      []ThreadArchiveFile{},
	}
	rootDir, err := resolveThreadArchiveRootDir()
	if err != nil {
		return manifest, apperrors.Wrap(err, "Server.archiveThreadArtifacts", "resolve archive root")
	}
	archiveDir, err := resolveThreadArchiveSnapshotDir(rootDir, id, manifest.ArchivedAt)
	if err != nil {
		return manifest, apperrors.Wrap(err, "Server.archiveThreadArtifacts", "resolve archive dir")
	}
	manifest.ArchiveDir = archiveDir
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		return manifest, apperrors.Wrap(err, "Server.archiveThreadArtifacts", "ensure archive dir")
	}

	codexThreadID, rolloutPath := a.ResolveRolloutHistorySource(ctx, id, NormalizeCodexThreadID)
	manifest.CodexThreadID = NormalizeCodexThreadID(codexThreadID)
	candidates := CollectThreadArtifactCandidates(manifest.CodexThreadID, rolloutPath)

	for _, candidate := range candidates {
		srcPath := strings.TrimSpace(candidate.Path)
		if srcPath == "" {
			continue
		}
		info, err := os.Stat(srcPath)
		if err != nil || info.IsDir() {
			continue
		}
		targetPath, err := nextArchiveFilePath(archiveDir, filepath.Base(srcPath))
		if err != nil {
			return manifest, apperrors.Wrap(err, "Server.archiveThreadArtifacts", "resolve archive target")
		}
		if err := copyFile(srcPath, targetPath); err != nil {
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
		checksum, err := fileSHA256(targetPath)
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

	if err := WriteThreadArchiveManifest(manifest); err != nil {
		return manifest, apperrors.Wrap(err, "Server.archiveThreadArtifacts", "write manifest")
	}
	a.bindRolloutPath(ctx, id, manifest.CodexThreadID, manifest.RolloutPath)
	a.pruneArchivedCodexSourceFiles(id, manifest.Files, manifest.ArchiveDir)
	return manifest, nil
}
