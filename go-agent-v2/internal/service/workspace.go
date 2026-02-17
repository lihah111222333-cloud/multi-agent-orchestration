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
		return nil, fmt.Errorf("workspace manager: run store is required")
	}
	if strings.TrimSpace(rootDir) == "" {
		rootDir = ".agent/workspaces"
	}
	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, fmt.Errorf("workspace manager: abs root: %w", err)
	}
	if err := os.MkdirAll(absRoot, 0o750); err != nil {
		return nil, fmt.Errorf("workspace manager: create root: %w", err)
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

func (m *WorkspaceManager) CreateRun(ctx context.Context, req WorkspaceCreateRequest) (*store.WorkspaceRun, error) {
	runKey := strings.TrimSpace(req.RunKey)
	if runKey == "" {
		runKey = fmt.Sprintf("run-%d", time.Now().UnixMilli())
	}
	if !workspaceRunKeyRe.MatchString(runKey) {
		return nil, fmt.Errorf("workspace/create: invalid runKey %q", runKey)
	}

	sourceRoot, err := filepath.Abs(strings.TrimSpace(req.SourceRoot))
	if err != nil {
		return nil, fmt.Errorf("workspace/create: invalid sourceRoot: %w", err)
	}
	stat, err := os.Stat(sourceRoot)
	if err != nil {
		return nil, fmt.Errorf("workspace/create: stat sourceRoot: %w", err)
	}
	if !stat.IsDir() {
		return nil, fmt.Errorf("workspace/create: sourceRoot must be directory")
	}

	runBase := filepath.Join(m.rootDir, runKey)
	if !isPathWithinRoot(m.rootDir, runBase) {
		return nil, fmt.Errorf("workspace/create: run path escapes workspace root")
	}
	workspacePath := filepath.Join(runBase, "workspace")
	if err := os.MkdirAll(workspacePath, 0o750); err != nil {
		return nil, fmt.Errorf("workspace/create: create workspace dir: %w", err)
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
		return nil, fmt.Errorf("workspace/create: save run: %w", err)
	}

	copied, copiedBytes, err := m.bootstrapFiles(ctx, saved, req.Files)
	if err != nil {
		_, _ = m.runs.UpdateRunStatus(ctx, saved.RunKey, WorkspaceRunStatusFailed, req.CreatedBy, map[string]any{
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
		return nil, fmt.Errorf("workspace/create: finalize run metadata: %w", err)
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
		return "", fmt.Errorf("workspace/run %q not found", runKey)
	}
	if run.Status == WorkspaceRunStatusAborted || run.Status == WorkspaceRunStatusFailed {
		return "", fmt.Errorf("workspace/run %q is %s", runKey, run.Status)
	}
	path, err := filepath.Abs(run.WorkspacePath)
	if err != nil {
		return "", err
	}
	if !isPathWithinRoot(m.rootDir, path) {
		return "", fmt.Errorf("workspace/run %q path escapes workspace root", runKey)
	}
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("workspace/run %q workspace path unavailable: %w", runKey, err)
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
		return nil, fmt.Errorf("workspace/merge: runKey is required")
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
		return nil, fmt.Errorf("workspace/merge: transition to merging: %w", err)
	}
	if !transitioned {
		current, err := m.runs.GetRun(ctx, runKey)
		if err != nil {
			return nil, fmt.Errorf("workspace/merge: load run status: %w", err)
		}
		if current == nil {
			return nil, fmt.Errorf("workspace/merge: run %q not found", runKey)
		}
		return nil, fmt.Errorf("workspace/merge: run %q status is %s, expected %s",
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
		_, _ = m.runs.UpdateRunStatus(ctx, run.RunKey, finalStatus, updatedBy, map[string]any{
			"dry_run":   req.DryRun,
			"merged":    result.Merged,
			"conflicts": result.Conflicts,
			"unchanged": result.Unchanged,
			"errors":    result.Errors,
		})
	}()

	trackedRows, err := m.runs.ListFiles(ctx, run.RunKey, "", m.maxFiles*4)
	if err != nil {
		return nil, fmt.Errorf("workspace/merge: list tracked files: %w", err)
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
			return fmt.Errorf("workspace/merge: too many files in workspace (%d > %d)", fileCount, m.maxFiles)
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
			_, _ = m.runs.SaveFile(ctx, &store.WorkspaceRunFile{
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
			_, _ = m.runs.SaveFile(ctx, &store.WorkspaceRunFile{
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
			_, _ = m.runs.SaveFile(ctx, &store.WorkspaceRunFile{
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
			_, _ = m.runs.SaveFile(ctx, &store.WorkspaceRunFile{
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
			_, _ = m.runs.SaveFile(ctx, &store.WorkspaceRunFile{
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
			_, _ = m.runs.SaveFile(ctx, &store.WorkspaceRunFile{
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
			_, _ = m.runs.SaveFile(ctx, &store.WorkspaceRunFile{
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
			_, _ = m.runs.SaveFile(ctx, &store.WorkspaceRunFile{
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
			_, _ = m.runs.SaveFile(ctx, &store.WorkspaceRunFile{
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
		_, _ = m.runs.SaveFile(ctx, &store.WorkspaceRunFile{
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
				_, _ = m.runs.SaveFile(ctx, &store.WorkspaceRunFile{
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
				_, _ = m.runs.SaveFile(ctx, &store.WorkspaceRunFile{
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
				_, _ = m.runs.SaveFile(ctx, &store.WorkspaceRunFile{
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
			_, _ = m.runs.SaveFile(ctx, &store.WorkspaceRunFile{
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
			return copied, totalBytes, fmt.Errorf("workspace/create: invalid bootstrap file %q: %w", raw, err)
		}
		if _, ok := seen[rel]; ok {
			continue
		}
		seen[rel] = struct{}{}
		if len(seen) > m.maxFiles {
			return copied, totalBytes, fmt.Errorf("workspace/create: bootstrap files exceed limit (%d)", m.maxFiles)
		}

		sourcePath := filepath.Join(run.SourceRoot, rel)
		if !isPathWithinRoot(run.SourceRoot, sourcePath) {
			return copied, totalBytes, fmt.Errorf("workspace/create: bootstrap path escapes source root: %s", rel)
		}

		info, err := os.Lstat(sourcePath)
		if err != nil {
			return copied, totalBytes, fmt.Errorf("workspace/create: bootstrap stat %s: %w", rel, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return copied, totalBytes, fmt.Errorf("workspace/create: bootstrap symlink not allowed: %s", rel)
		}
		if info.IsDir() {
			return copied, totalBytes, fmt.Errorf("workspace/create: bootstrap path is directory: %s", rel)
		}
		if info.Size() > m.maxFileBytes {
			return copied, totalBytes, fmt.Errorf("workspace/create: bootstrap file too large %s (%d bytes > %d)",
				rel, info.Size(), m.maxFileBytes)
		}
		totalBytes += info.Size()
		if totalBytes > m.maxTotalBytes {
			return copied, totalBytes, fmt.Errorf("workspace/create: total bootstrap bytes exceed limit (%d > %d)",
				totalBytes, m.maxTotalBytes)
		}

		targetPath := filepath.Join(run.WorkspacePath, rel)
		if !isPathWithinRoot(run.WorkspacePath, targetPath) {
			return copied, totalBytes, fmt.Errorf("workspace/create: bootstrap target escapes workspace: %s", rel)
		}
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o750); err != nil {
			return copied, totalBytes, fmt.Errorf("workspace/create: mkdir target dir for %s: %w", rel, err)
		}
		if err := copyFileAtomic(sourcePath, targetPath, info.Mode().Perm()); err != nil {
			return copied, totalBytes, fmt.Errorf("workspace/create: copy bootstrap file %s: %w", rel, err)
		}

		hash, err := hashFile(sourcePath)
		if err != nil {
			return copied, totalBytes, fmt.Errorf("workspace/create: hash bootstrap source %s: %w", rel, err)
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
			return copied, totalBytes, fmt.Errorf("workspace/create: save bootstrap file state %s: %w", rel, err)
		}
		copied++
	}
	return copied, totalBytes, nil
}

func normalizeRelativePath(path string) (string, error) {
	p := filepath.Clean(strings.TrimSpace(path))
	if p == "" || p == "." {
		return "", fmt.Errorf("path is empty")
	}
	if filepath.IsAbs(p) {
		return "", fmt.Errorf("absolute path is not allowed")
	}
	if p == ".." || strings.HasPrefix(p, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path traversal is not allowed")
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
		return fmt.Errorf("target is symlink: %s", target)
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
