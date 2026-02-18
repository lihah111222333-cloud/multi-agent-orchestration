package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/store"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

const (
	WorkspaceRunStatusActive  = "active"
	WorkspaceRunStatusMerging = "merging"
	WorkspaceRunStatusMerged  = "merged"
	WorkspaceRunStatusAborted = "aborted"
	WorkspaceRunStatusFailed  = "failed"
)

const (
	WorkspaceFileStateTracked   = "tracked"
	WorkspaceFileStateSynced    = "synced"
	WorkspaceFileStateChanged   = "changed"
	WorkspaceFileStateMerged    = "merged"
	WorkspaceFileStateConflict  = "conflict"
	WorkspaceFileStateError     = "error"
	WorkspaceFileStateUnchanged = "unchanged"
)

var workspaceRunKeyRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{2,127}$`)

type WorkspaceManager struct {
	runs          *store.WorkspaceRunStore
	rootDir       string
	maxFiles      int
	maxFileBytes  int64
	maxTotalBytes int64
}

type WorkspaceCreateRequest struct {
	RunKey     string   `json:"runKey"`
	DagKey     string   `json:"dagKey"`
	SourceRoot string   `json:"sourceRoot"`
	CreatedBy  string   `json:"createdBy"`
	Files      []string `json:"files"`
	Metadata   any      `json:"metadata"`
}

type WorkspaceMergeRequest struct {
	RunKey        string `json:"runKey"`
	UpdatedBy     string `json:"updatedBy"`
	DryRun        bool   `json:"dryRun"`
	DeleteRemoved bool   `json:"deleteRemoved"`
}

type WorkspaceMergeFileResult struct {
	Path   string `json:"path"`
	Action string `json:"action"` // merged|would_merge|deleted|would_delete|conflict|error|unchanged
	Reason string `json:"reason,omitempty"`
}

type WorkspaceMergeResult struct {
	RunKey     string                     `json:"runKey"`
	Status     string                     `json:"status"`
	Workspace  string                     `json:"workspace"`
	SourceRoot string                     `json:"sourceRoot"`
	DryRun     bool                       `json:"dryRun"`
	Merged     int                        `json:"merged"`
	Conflicts  int                        `json:"conflicts"`
	Unchanged  int                        `json:"unchanged"`
	Errors     int                        `json:"errors"`
	Files      []WorkspaceMergeFileResult `json:"files"`
	FinishedAt time.Time                  `json:"finishedAt"`
}

func NewWorkspaceManager(
	runStore *store.WorkspaceRunStore,
	rootDir string,
	maxFiles int,
	maxFileBytes int64,
	maxTotalBytes int64,
) (*WorkspaceManager, error) {
	if runStore == nil {
		return nil, apperrors.New("NewWorkspaceManager", "run store is required")
	}
	if strings.TrimSpace(rootDir) == "" {
		rootDir = ".agent/workspaces"
	}
	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, apperrors.Wrap(err, "NewWorkspaceManager", "abs root")
	}
	if err := os.MkdirAll(absRoot, 0o750); err != nil {
		return nil, apperrors.Wrap(err, "NewWorkspaceManager", "create root dir")
	}
	if maxFiles <= 0 {
		maxFiles = 5000
	}
	if maxFileBytes <= 0 {
		maxFileBytes = 8 << 20 // 8MB
	}
	if maxTotalBytes <= 0 {
		maxTotalBytes = 256 << 20 // 256MB
	}

	return &WorkspaceManager{
		runs:          runStore,
		rootDir:       absRoot,
		maxFiles:      maxFiles,
		maxFileBytes:  maxFileBytes,
		maxTotalBytes: maxTotalBytes,
	}, nil
}

func (m *WorkspaceManager) RootDir() string { return m.rootDir }

// saveFileOrLog 保存文件状态记录, 失败时记录日志 (不中断流程)。
func (m *WorkspaceManager) saveFileOrLog(ctx context.Context, file *store.WorkspaceRunFile) {
	if _, err := m.runs.SaveFile(ctx, file); err != nil {
		logger.Warn("workspace: save file state failed",
			logger.FieldRunKey, file.RunKey,
			logger.FieldPath, file.RelativePath,
			logger.FieldStatus, file.State,
			logger.FieldError, err,
		)
	}
}

// updateRunStatusOrLog 更新 run 状态, 失败时记录日志 (不中断流程)。
func (m *WorkspaceManager) updateRunStatusOrLog(ctx context.Context, runKey, status, updatedBy string, meta map[string]any) {
	if _, err := m.runs.UpdateRunStatus(ctx, runKey, status, updatedBy, meta); err != nil {
		logger.Warn("workspace: update run status failed",
			logger.FieldRunKey, runKey,
			logger.FieldStatus, status,
			logger.FieldError, err,
		)
	}
}

func (m *WorkspaceManager) CreateRun(ctx context.Context, req WorkspaceCreateRequest) (*store.WorkspaceRun, error) {
	runKey := strings.TrimSpace(req.RunKey)
	if runKey == "" {
		runKey = fmt.Sprintf("run-%d", time.Now().UnixMilli())
	}
	if !workspaceRunKeyRe.MatchString(runKey) {
		return nil, apperrors.Newf("WorkspaceManager.CreateRun", "invalid runKey %q", runKey)
	}

	sourceRoot, err := filepath.Abs(strings.TrimSpace(req.SourceRoot))
	if err != nil {
		return nil, apperrors.Wrap(err, "WorkspaceManager.CreateRun", "invalid sourceRoot")
	}
	stat, err := os.Stat(sourceRoot)
	if err != nil {
		return nil, apperrors.Wrap(err, "WorkspaceManager.CreateRun", "stat sourceRoot")
	}
	if !stat.IsDir() {
		return nil, apperrors.New("WorkspaceManager.CreateRun", "sourceRoot must be directory")
	}

	runBase := filepath.Join(m.rootDir, runKey)
	if !isPathWithinRoot(m.rootDir, runBase) {
		return nil, apperrors.New("WorkspaceManager.CreateRun", "run path escapes workspace root")
	}
	workspacePath := filepath.Join(runBase, "workspace")
	if err := os.MkdirAll(workspacePath, 0o750); err != nil {
		return nil, apperrors.Wrap(err, "WorkspaceManager.CreateRun", "create workspace dir")
	}

	run := &store.WorkspaceRun{
		RunKey:        runKey,
		DagKey:        strings.TrimSpace(req.DagKey),
		SourceRoot:    sourceRoot,
		WorkspacePath: workspacePath,
		Status:        WorkspaceRunStatusActive,
		CreatedBy:     strings.TrimSpace(req.CreatedBy),
		UpdatedBy:     strings.TrimSpace(req.CreatedBy),
		Metadata:      req.Metadata,
	}
	saved, err := m.runs.SaveRun(ctx, run)
	if err != nil {
		return nil, apperrors.Wrap(err, "WorkspaceManager.CreateRun", "save run")
	}

	copied, copiedBytes, err := m.bootstrapFiles(ctx, saved, req.Files)
	if err != nil {
		m.updateRunStatusOrLog(ctx, saved.RunKey, WorkspaceRunStatusFailed, req.CreatedBy, map[string]any{
			"error": err.Error(),
		})
		return nil, err
	}

	meta := mergeMetadata(req.Metadata, map[string]any{
		"bootstrap_files": copied,
		"bootstrap_bytes": copiedBytes,
	})
	saved.Metadata = meta
	saved.UpdatedBy = req.CreatedBy
	saved, err = m.runs.SaveRun(ctx, saved)
	if err != nil {
		return nil, apperrors.Wrap(err, "WorkspaceManager.CreateRun", "finalize run metadata")
	}
	return saved, nil
}

func (m *WorkspaceManager) GetRun(ctx context.Context, runKey string) (*store.WorkspaceRun, error) {
	return m.runs.GetRun(ctx, strings.TrimSpace(runKey))
}

func (m *WorkspaceManager) ListRuns(ctx context.Context, status, dagKey string, limit int) ([]store.WorkspaceRun, error) {
	return m.runs.ListRuns(ctx, strings.TrimSpace(status), strings.TrimSpace(dagKey), limit)
}

func (m *WorkspaceManager) ResolveRunWorkspace(ctx context.Context, runKey string) (string, error) {
	run, err := m.runs.GetRun(ctx, strings.TrimSpace(runKey))
	if err != nil {
		return "", err
	}
	if run == nil {
		return "", apperrors.Newf("WorkspaceManager.ResolveRunWorkspace", "run %q not found", runKey)
	}
	if run.Status == WorkspaceRunStatusAborted || run.Status == WorkspaceRunStatusFailed {
		return "", apperrors.Newf("WorkspaceManager.ResolveRunWorkspace", "run %q is %s", runKey, run.Status)
	}
	path, err := filepath.Abs(run.WorkspacePath)
	if err != nil {
		return "", err
	}
	if !isPathWithinRoot(m.rootDir, path) {
		return "", apperrors.Newf("WorkspaceManager.ResolveRunWorkspace", "run %q path escapes workspace root", runKey)
	}
	if _, err := os.Stat(path); err != nil {
		return "", apperrors.Wrapf(err, "WorkspaceManager.ResolveRunWorkspace", "run %q workspace path unavailable", runKey)
	}
	return path, nil
}

func (m *WorkspaceManager) AbortRun(ctx context.Context, runKey, updatedBy, reason string) (*store.WorkspaceRun, error) {
	return m.runs.UpdateRunStatus(ctx, strings.TrimSpace(runKey), WorkspaceRunStatusAborted, updatedBy, map[string]any{
		"reason": strings.TrimSpace(reason),
	})
}

func (m *WorkspaceManager) MergeRun(ctx context.Context, req WorkspaceMergeRequest) (*WorkspaceMergeResult, error) {
	runKey := strings.TrimSpace(req.RunKey)
	if runKey == "" {
		return nil, apperrors.New("WorkspaceManager.MergeRun", "runKey is required")
	}
	updatedBy := strings.TrimSpace(req.UpdatedBy)

	run, transitioned, err := m.runs.TryTransitionRunStatus(
		ctx,
		runKey,
		WorkspaceRunStatusActive,
		WorkspaceRunStatusMerging,
		updatedBy,
		map[string]any{"dry_run": req.DryRun, "started_at": time.Now().Format(time.RFC3339)},
	)
	if err != nil {
		return nil, apperrors.Wrap(err, "WorkspaceManager.MergeRun", "transition to merging")
	}
	if !transitioned {
		current, err := m.runs.GetRun(ctx, runKey)
		if err != nil {
			return nil, apperrors.Wrap(err, "WorkspaceManager.MergeRun", "load run status")
		}
		if current == nil {
			return nil, apperrors.Newf("WorkspaceManager.MergeRun", "run %q not found", runKey)
		}
		return nil, apperrors.Newf("WorkspaceManager.MergeRun", "run %q status is %s, expected %s",
			runKey, current.Status, WorkspaceRunStatusActive)
	}

	result := &WorkspaceMergeResult{
		RunKey:     run.RunKey,
		Status:     WorkspaceRunStatusMerging,
		Workspace:  run.WorkspacePath,
		SourceRoot: run.SourceRoot,
		DryRun:     req.DryRun,
		Files:      make([]WorkspaceMergeFileResult, 0, 64),
	}

	finalStatus := WorkspaceRunStatusMerged
	defer func() {
		if req.DryRun {
			finalStatus = WorkspaceRunStatusActive
		} else if result.Errors > 0 || result.Conflicts > 0 {
			finalStatus = WorkspaceRunStatusFailed
		}
		result.Status = finalStatus
		result.FinishedAt = time.Now()
		m.updateRunStatusOrLog(ctx, run.RunKey, finalStatus, updatedBy, map[string]any{
			"dry_run":   req.DryRun,
			"merged":    result.Merged,
			"conflicts": result.Conflicts,
			"unchanged": result.Unchanged,
			"errors":    result.Errors,
		})
	}()

	trackedRows, err := m.runs.ListFiles(ctx, run.RunKey, "", m.maxFiles*4)
	if err != nil {
		return nil, apperrors.Wrap(err, "WorkspaceManager.MergeRun", "list tracked files")
	}
	tracked := make(map[string]store.WorkspaceRunFile, len(trackedRows))
	for _, row := range trackedRows {
		tracked[row.RelativePath] = row
	}
	seen := make(map[string]bool, len(trackedRows))

	fileCount := 0
	err = filepath.WalkDir(run.WorkspacePath, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			result.Errors++
			result.Files = append(result.Files, WorkspaceMergeFileResult{
				Path:   path,
				Action: "error",
				Reason: walkErr.Error(),
			})
			return nil
		}
		if d.IsDir() {
			return nil
		}
		fileCount++
		if fileCount > m.maxFiles {
			return apperrors.Newf("WorkspaceManager.MergeRun", "too many files in workspace (%d > %d)", fileCount, m.maxFiles)
		}
		if d.Type()&os.ModeSymlink != 0 {
			result.Errors++
			rel, _ := filepath.Rel(run.WorkspacePath, path)
			result.Files = append(result.Files, WorkspaceMergeFileResult{
				Path:   rel,
				Action: "error",
				Reason: "symlink is not allowed in workspace",
			})
			return nil
		}

		relRaw, err := filepath.Rel(run.WorkspacePath, path)
		if err != nil {
			result.Errors++
			return nil
		}
		rel, err := normalizeRelativePath(relRaw)
		if err != nil {
			result.Errors++
			result.Files = append(result.Files, WorkspaceMergeFileResult{Path: relRaw, Action: "error", Reason: err.Error()})
			return nil
		}
		seen[rel] = true

		wsInfo, err := os.Stat(path)
		if err != nil {
			result.Errors++
			result.Files = append(result.Files, WorkspaceMergeFileResult{Path: rel, Action: "error", Reason: err.Error()})
			return nil
		}
		if wsInfo.Size() > m.maxFileBytes {
			result.Errors++
			msg := fmt.Sprintf("workspace file too large: %d bytes > %d", wsInfo.Size(), m.maxFileBytes)
			m.saveFileOrLog(ctx, &store.WorkspaceRunFile{
				RunKey:       run.RunKey,
				RelativePath: rel,
				State:        WorkspaceFileStateError,
				LastError:    msg,
			})
			result.Files = append(result.Files, WorkspaceMergeFileResult{Path: rel, Action: "error", Reason: msg})
			return nil
		}

		wsHash, err := hashFile(path)
		if err != nil {
			result.Errors++
			m.saveFileOrLog(ctx, &store.WorkspaceRunFile{
				RunKey:       run.RunKey,
				RelativePath: rel,
				State:        WorkspaceFileStateError,
				LastError:    err.Error(),
			})
			result.Files = append(result.Files, WorkspaceMergeFileResult{Path: rel, Action: "error", Reason: err.Error()})
			return nil
		}

		sourcePath := filepath.Join(run.SourceRoot, rel)
		if !isPathWithinRoot(run.SourceRoot, sourcePath) {
			result.Errors++
			msg := "target path escapes source root"
			m.saveFileOrLog(ctx, &store.WorkspaceRunFile{
				RunKey:          run.RunKey,
				RelativePath:    rel,
				WorkspaceSHA256: wsHash,
				State:           WorkspaceFileStateError,
				LastError:       msg,
			})
			result.Files = append(result.Files, WorkspaceMergeFileResult{Path: rel, Action: "error", Reason: msg})
			return nil
		}

		sourceBefore, err := hashFileIfExists(sourcePath)
		if err != nil {
			result.Errors++
			m.saveFileOrLog(ctx, &store.WorkspaceRunFile{
				RunKey:             run.RunKey,
				RelativePath:       rel,
				WorkspaceSHA256:    wsHash,
				SourceSHA256Before: sourceBefore,
				State:              WorkspaceFileStateError,
				LastError:          err.Error(),
			})
			result.Files = append(result.Files, WorkspaceMergeFileResult{Path: rel, Action: "error", Reason: err.Error()})
			return nil
		}

		trackedFile, exists := tracked[rel]
		baseline := ""
		if exists {
			baseline = trackedFile.BaselineSHA256
		} else {
			baseline = sourceBefore
		}

		if baseline != "" && wsHash == baseline {
			result.Unchanged++
			m.saveFileOrLog(ctx, &store.WorkspaceRunFile{
				RunKey:             run.RunKey,
				RelativePath:       rel,
				BaselineSHA256:     baseline,
				WorkspaceSHA256:    wsHash,
				SourceSHA256Before: sourceBefore,
				SourceSHA256After:  sourceBefore,
				State:              WorkspaceFileStateUnchanged,
			})
			result.Files = append(result.Files, WorkspaceMergeFileResult{Path: rel, Action: "unchanged"})
			return nil
		}

		if baseline != "" && sourceBefore != "" && sourceBefore != baseline {
			result.Conflicts++
			reason := "source changed since baseline"
			m.saveFileOrLog(ctx, &store.WorkspaceRunFile{
				RunKey:             run.RunKey,
				RelativePath:       rel,
				BaselineSHA256:     baseline,
				WorkspaceSHA256:    wsHash,
				SourceSHA256Before: sourceBefore,
				State:              WorkspaceFileStateConflict,
				LastError:          reason,
			})
			result.Files = append(result.Files, WorkspaceMergeFileResult{Path: rel, Action: "conflict", Reason: reason})
			return nil
		}

		if req.DryRun {
			m.saveFileOrLog(ctx, &store.WorkspaceRunFile{
				RunKey:             run.RunKey,
				RelativePath:       rel,
				BaselineSHA256:     baseline,
				WorkspaceSHA256:    wsHash,
				SourceSHA256Before: sourceBefore,
				State:              WorkspaceFileStateChanged,
			})
			result.Merged++
			result.Files = append(result.Files, WorkspaceMergeFileResult{Path: rel, Action: "would_merge"})
			return nil
		}

		if err := copyFileAtomic(path, sourcePath, wsInfo.Mode().Perm()); err != nil {
			result.Errors++
			m.saveFileOrLog(ctx, &store.WorkspaceRunFile{
				RunKey:             run.RunKey,
				RelativePath:       rel,
				BaselineSHA256:     baseline,
				WorkspaceSHA256:    wsHash,
				SourceSHA256Before: sourceBefore,
				State:              WorkspaceFileStateError,
				LastError:          err.Error(),
			})
			result.Files = append(result.Files, WorkspaceMergeFileResult{Path: rel, Action: "error", Reason: err.Error()})
			return nil
		}

		sourceAfter, hashErr := hashFileIfExists(sourcePath)
		if hashErr != nil {
			result.Errors++
			m.saveFileOrLog(ctx, &store.WorkspaceRunFile{
				RunKey:             run.RunKey,
				RelativePath:       rel,
				BaselineSHA256:     baseline,
				WorkspaceSHA256:    wsHash,
				SourceSHA256Before: sourceBefore,
				State:              WorkspaceFileStateError,
				LastError:          hashErr.Error(),
			})
			result.Files = append(result.Files, WorkspaceMergeFileResult{Path: rel, Action: "error", Reason: hashErr.Error()})
			return nil
		}

		result.Merged++
		m.saveFileOrLog(ctx, &store.WorkspaceRunFile{
			RunKey:             run.RunKey,
			RelativePath:       rel,
			BaselineSHA256:     baseline,
			WorkspaceSHA256:    wsHash,
			SourceSHA256Before: sourceBefore,
			SourceSHA256After:  sourceAfter,
			State:              WorkspaceFileStateMerged,
		})
		result.Files = append(result.Files, WorkspaceMergeFileResult{Path: rel, Action: "merged"})
		return nil
	})
	if err != nil {
		return nil, err
	}

	if req.DeleteRemoved {
		for rel, trackedFile := range tracked {
			if seen[rel] {
				continue
			}
			sourcePath := filepath.Join(run.SourceRoot, rel)
			sourceBefore, hashErr := hashFileIfExists(sourcePath)
			if hashErr != nil {
				result.Errors++
				m.saveFileOrLog(ctx, &store.WorkspaceRunFile{
					RunKey:             run.RunKey,
					RelativePath:       rel,
					BaselineSHA256:     trackedFile.BaselineSHA256,
					SourceSHA256Before: sourceBefore,
					State:              WorkspaceFileStateError,
					LastError:          hashErr.Error(),
				})
				result.Files = append(result.Files, WorkspaceMergeFileResult{Path: rel, Action: "error", Reason: hashErr.Error()})
				continue
			}
			if trackedFile.BaselineSHA256 != "" && sourceBefore != "" && sourceBefore != trackedFile.BaselineSHA256 {
				result.Conflicts++
				reason := "delete conflict: source changed since baseline"
				m.saveFileOrLog(ctx, &store.WorkspaceRunFile{
					RunKey:             run.RunKey,
					RelativePath:       rel,
					BaselineSHA256:     trackedFile.BaselineSHA256,
					SourceSHA256Before: sourceBefore,
					State:              WorkspaceFileStateConflict,
					LastError:          reason,
				})
				result.Files = append(result.Files, WorkspaceMergeFileResult{Path: rel, Action: "conflict", Reason: reason})
				continue
			}

			if req.DryRun {
				result.Merged++
				result.Files = append(result.Files, WorkspaceMergeFileResult{Path: rel, Action: "would_delete"})
				continue
			}

			if err := os.Remove(sourcePath); err != nil && !os.IsNotExist(err) {
				result.Errors++
				m.saveFileOrLog(ctx, &store.WorkspaceRunFile{
					RunKey:             run.RunKey,
					RelativePath:       rel,
					BaselineSHA256:     trackedFile.BaselineSHA256,
					SourceSHA256Before: sourceBefore,
					State:              WorkspaceFileStateError,
					LastError:          err.Error(),
				})
				result.Files = append(result.Files, WorkspaceMergeFileResult{Path: rel, Action: "error", Reason: err.Error()})
				continue
			}
			result.Merged++
			m.saveFileOrLog(ctx, &store.WorkspaceRunFile{
				RunKey:             run.RunKey,
				RelativePath:       rel,
				BaselineSHA256:     trackedFile.BaselineSHA256,
				SourceSHA256Before: sourceBefore,
				SourceSHA256After:  "",
				State:              WorkspaceFileStateMerged,
			})
			result.Files = append(result.Files, WorkspaceMergeFileResult{Path: rel, Action: "deleted"})
		}
	}

	return result, nil
}

func (m *WorkspaceManager) bootstrapFiles(ctx context.Context, run *store.WorkspaceRun, files []string) (int, int64, error) {
	if len(files) == 0 {
		return 0, 0, nil
	}
	seen := make(map[string]struct{}, len(files))
	totalBytes := int64(0)
	copied := 0

	for _, raw := range files {
		rel, err := normalizeRelativePath(raw)
		if err != nil {
			return copied, totalBytes, apperrors.Wrapf(err, "WorkspaceManager.bootstrapFiles", "invalid bootstrap file %q", raw)
		}
		if _, ok := seen[rel]; ok {
			continue
		}
		seen[rel] = struct{}{}
		if len(seen) > m.maxFiles {
			return copied, totalBytes, apperrors.Newf("WorkspaceManager.bootstrapFiles", "bootstrap files exceed limit (%d)", m.maxFiles)
		}

		sourcePath := filepath.Join(run.SourceRoot, rel)
		if !isPathWithinRoot(run.SourceRoot, sourcePath) {
			return copied, totalBytes, apperrors.Newf("WorkspaceManager.bootstrapFiles", "bootstrap path escapes source root: %s", rel)
		}

		info, err := os.Lstat(sourcePath)
		if err != nil {
			return copied, totalBytes, apperrors.Wrapf(err, "WorkspaceManager.bootstrapFiles", "bootstrap stat %s", rel)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return copied, totalBytes, apperrors.Newf("WorkspaceManager.bootstrapFiles", "bootstrap symlink not allowed: %s", rel)
		}
		if info.IsDir() {
			return copied, totalBytes, apperrors.Newf("WorkspaceManager.bootstrapFiles", "bootstrap path is directory: %s", rel)
		}
		if info.Size() > m.maxFileBytes {
			return copied, totalBytes, apperrors.Newf("WorkspaceManager.bootstrapFiles", "bootstrap file too large %s (%d bytes > %d)",
				rel, info.Size(), m.maxFileBytes)
		}
		totalBytes += info.Size()
		if totalBytes > m.maxTotalBytes {
			return copied, totalBytes, apperrors.Newf("WorkspaceManager.bootstrapFiles", "total bootstrap bytes exceed limit (%d > %d)",
				totalBytes, m.maxTotalBytes)
		}

		targetPath := filepath.Join(run.WorkspacePath, rel)
		if !isPathWithinRoot(run.WorkspacePath, targetPath) {
			return copied, totalBytes, apperrors.Newf("WorkspaceManager.bootstrapFiles", "bootstrap target escapes workspace: %s", rel)
		}
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o750); err != nil {
			return copied, totalBytes, apperrors.Wrapf(err, "WorkspaceManager.bootstrapFiles", "mkdir target dir for %s", rel)
		}
		if err := copyFileAtomic(sourcePath, targetPath, info.Mode().Perm()); err != nil {
			return copied, totalBytes, apperrors.Wrapf(err, "WorkspaceManager.bootstrapFiles", "copy bootstrap file %s", rel)
		}

		hash, err := hashFile(sourcePath)
		if err != nil {
			return copied, totalBytes, apperrors.Wrapf(err, "WorkspaceManager.bootstrapFiles", "hash bootstrap source %s", rel)
		}
		_, err = m.runs.SaveFile(ctx, &store.WorkspaceRunFile{
			RunKey:             run.RunKey,
			RelativePath:       rel,
			BaselineSHA256:     hash,
			WorkspaceSHA256:    hash,
			SourceSHA256Before: hash,
			SourceSHA256After:  hash,
			State:              WorkspaceFileStateSynced,
		})
		if err != nil {
			return copied, totalBytes, apperrors.Wrapf(err, "WorkspaceManager.bootstrapFiles", "save bootstrap file state %s", rel)
		}
		copied++
	}
	return copied, totalBytes, nil
}

func normalizeRelativePath(path string) (string, error) {
	p := filepath.Clean(strings.TrimSpace(path))
	if p == "" || p == "." {
		return "", apperrors.New("normalizeRelativePath", "path is empty")
	}
	if filepath.IsAbs(p) {
		return "", apperrors.New("normalizeRelativePath", "absolute path is not allowed")
	}
	if p == ".." || strings.HasPrefix(p, ".."+string(os.PathSeparator)) {
		return "", apperrors.New("normalizeRelativePath", "path traversal is not allowed")
	}
	return p, nil
}

func isPathWithinRoot(root, path string) bool {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(rootAbs, pathAbs)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func copyFileAtomic(source, target string, perm os.FileMode) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		return err
	}

	if stat, err := os.Lstat(target); err == nil && stat.Mode()&os.ModeSymlink != 0 {
		return apperrors.Newf("copyFileAtomic", "target is symlink: %s", target)
	}

	tmp, err := os.CreateTemp(filepath.Dir(target), ".workspace-merge-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := io.Copy(tmp, in); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, perm); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, target); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func hashFileIfExists(path string) (string, error) {
	_, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return hashFile(path)
}

func mergeMetadata(base any, extra map[string]any) map[string]any {
	out := map[string]any{}
	if m, ok := base.(map[string]any); ok {
		for k, v := range m {
			out[k] = v
		}
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}
