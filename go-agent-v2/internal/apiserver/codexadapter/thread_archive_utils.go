package codexadapter

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
)

// NormalizeThreadArchiveMap normalizes archive state payload into map[string]int64.
func NormalizeThreadArchiveMap(value any) map[string]int64 {
	result := map[string]int64{}

	switch typed := value.(type) {
	case map[string]int64:
		for id, at := range typed {
			addThreadArchiveMapEntry(result, id, at)
		}
	case map[string]any:
		for id, at := range typed {
			addThreadArchiveMapEntry(result, id, at)
		}
	case string:
		decoded := map[string]any{}
		if err := json.Unmarshal([]byte(strings.TrimSpace(typed)), &decoded); err == nil {
			for id, at := range decoded {
				addThreadArchiveMapEntry(result, id, at)
			}
		}
	case json.RawMessage:
		decoded := map[string]any{}
		if err := json.Unmarshal(typed, &decoded); err == nil {
			for id, at := range decoded {
				addThreadArchiveMapEntry(result, id, at)
			}
		}
	}

	return result
}

func addThreadArchiveMapEntry(result map[string]int64, rawID string, rawAt any) {
	if result == nil {
		return
	}
	id := strings.TrimSpace(rawID)
	if id == "" {
		return
	}
	at := normalizeThreadArchiveTimestamp(rawAt)
	if at <= 0 {
		return
	}
	result[id] = at
}

func normalizeThreadArchiveTimestamp(rawAt any) int64 {
	switch v := rawAt.(type) {
	case int:
		return int64(v)
	case int64:
		return v
	case float64:
		return int64(v)
	case json.Number:
		parsed, err := v.Int64()
		if err == nil {
			return parsed
		}
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err == nil {
			return parsed
		}
	}
	return 0
}

func allocateUniquePath(primary string, next func(index int) string) (string, error) {
	if _, err := os.Stat(primary); os.IsNotExist(err) {
		return primary, nil
	} else if err != nil {
		return "", err
	}
	for i := 2; i <= 9999; i++ {
		candidate := next(i)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate, nil
		} else if err != nil {
			return "", err
		}
	}
	return "", os.ErrExist
}

// resolveThreadArchiveRootDir resolves and ensures ~/.multi-agent/thread-archives.
func resolveThreadArchiveRootDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", apperrors.Wrap(err, "resolveThreadArchiveRootDir", "resolve user home")
	}
	homeDir = strings.TrimSpace(homeDir)
	if homeDir == "" {
		return "", apperrors.New("resolveThreadArchiveRootDir", "user home is empty")
	}
	appRootDir := filepath.Join(homeDir, ".multi-agent")
	if err := os.MkdirAll(appRootDir, 0o755); err != nil {
		return "", apperrors.Wrap(err, "resolveThreadArchiveRootDir", "ensure app root")
	}
	archiveRoot := filepath.Join(appRootDir, "thread-archives")
	if err := os.MkdirAll(archiveRoot, 0o755); err != nil {
		return "", apperrors.Wrap(err, "resolveThreadArchiveRootDir", "ensure archive root")
	}
	return archiveRoot, nil
}

// resolveThreadArchiveSnapshotDir resolves unique snapshot dir for archived thread.
func resolveThreadArchiveSnapshotDir(rootDir string, threadID string, archivedAt string) (string, error) {
	safeThreadID, err := SanitizeArchiveNameStrict(threadID)
	if err != nil {
		return "", apperrors.Wrap(err, "resolveThreadArchiveSnapshotDir", "sanitize thread id")
	}
	threadDir := filepath.Join(rootDir, safeThreadID)
	if err := os.MkdirAll(threadDir, 0o755); err != nil {
		return "", apperrors.Wrap(err, "resolveThreadArchiveSnapshotDir", "ensure thread dir")
	}
	snapshotName, err := SanitizeArchiveNameStrict(strings.TrimSpace(archivedAt))
	if err != nil {
		return "", apperrors.Wrap(err, "resolveThreadArchiveSnapshotDir", "sanitize archive timestamp")
	}
	snapshotDir := filepath.Join(threadDir, snapshotName)
	resolved, err := allocateUniquePath(snapshotDir, func(i int) string {
		return filepath.Join(threadDir, fmt.Sprintf("%s-%d", snapshotName, i))
	})
	if err == nil {
		return resolved, nil
	}
	if errors.Is(err, os.ErrExist) {
		return "", apperrors.New("resolveThreadArchiveSnapshotDir", "unable to allocate unique archive snapshot dir")
	}
	return "", apperrors.Wrap(err, "resolveThreadArchiveSnapshotDir", "stat snapshot candidate")
}

// SanitizeArchiveName sanitizes archive file and directory names.
func SanitizeArchiveName(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(trimmed))
	for _, r := range trimmed {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "._")
}

// SanitizeArchiveNameStrict validates sanitized archive names.
func SanitizeArchiveNameStrict(raw string) (string, error) {
	sanitized := SanitizeArchiveName(raw)
	if sanitized == "" {
		return "", apperrors.Newf("sanitizeArchiveNameStrict", "invalid archive name from %q", raw)
	}
	return sanitized, nil
}

// nextArchiveFilePath allocates a unique archive file path in target dir.
func nextArchiveFilePath(dir, filename string) (string, error) {
	base, err := SanitizeArchiveNameStrict(filepath.Base(filename))
	if err != nil {
		return "", apperrors.Wrap(err, "nextArchiveFilePath", "sanitize filename")
	}
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	candidate := filepath.Join(dir, base)
	resolved, err := allocateUniquePath(candidate, func(i int) string {
		return filepath.Join(dir, fmt.Sprintf("%s-%d%s", stem, i, ext))
	})
	if err == nil {
		return resolved, nil
	}
	if errors.Is(err, os.ErrExist) {
		return "", apperrors.New("nextArchiveFilePath", "unable to allocate unique archive filename")
	}
	return "", apperrors.Wrap(err, "nextArchiveFilePath", "stat archive target candidate")
}

func copyFileAtomic(srcPath, targetPath string, overwrite bool) error {
	op := "copyFile"
	if overwrite {
		op = "copyFileOverwrite"
	}
	if !overwrite {
		if _, err := os.Stat(targetPath); err == nil {
			return apperrors.Newf(op, "target already exists: %s", targetPath)
		} else if !os.IsNotExist(err) {
			return apperrors.Wrap(err, op, "stat target path")
		}
	}

	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()

	info, err := src.Stat()
	if err != nil {
		return err
	}
	mode := info.Mode().Perm()
	if mode == 0 {
		mode = 0o644
	}

	targetDir := filepath.Dir(targetPath)
	if overwrite {
		if err := os.MkdirAll(targetDir, 0o755); err != nil {
			return err
		}
	}
	tmpFile, err := os.CreateTemp(targetDir, "."+filepath.Base(targetPath)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	if _, err := io.Copy(tmpFile, src); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Chmod(tmpPath, mode); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, targetPath); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

// copyFile copies src to target and fails if target exists.
func copyFile(srcPath, targetPath string) error {
	return copyFileAtomic(srcPath, targetPath, false)
}

// copyFileOverwrite copies src to target with atomic overwrite semantics.
func copyFileOverwrite(srcPath, targetPath string) error {
	return copyFileAtomic(srcPath, targetPath, true)
}

// fileSHA256 computes file SHA256 checksum in hex.
func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// resolveCodexRootDir resolves ~/.codex root directory path.
func resolveCodexRootDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", apperrors.Wrap(err, "resolveCodexRootDir", "resolve user home")
	}
	homeDir = strings.TrimSpace(homeDir)
	if homeDir == "" {
		return "", apperrors.New("resolveCodexRootDir", "user home is empty")
	}
	return filepath.Join(homeDir, ".codex"), nil
}

// removeEmptyCodexParentDirs removes empty parent dirs until codex root boundary.
func removeEmptyCodexParentDirs(startDir string, codexRoot string) {
	current := strings.TrimSpace(startDir)
	root := strings.TrimSpace(codexRoot)
	if current == "" || root == "" {
		return
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return
	}
	for current != "" {
		currentAbs, err := filepath.Abs(current)
		if err != nil {
			return
		}
		if currentAbs == rootAbs {
			return
		}
		withinRoot, err := PathWithinRoot(rootAbs, currentAbs)
		if err != nil || !withinRoot {
			return
		}
		entries, err := os.ReadDir(currentAbs)
		if err != nil || len(entries) > 0 {
			return
		}
		if err := os.Remove(currentAbs); err != nil {
			return
		}
		parent := filepath.Dir(currentAbs)
		if parent == currentAbs {
			return
		}
		current = parent
	}
}

// PathWithinRoot returns whether path is inside root (or equal root).
func PathWithinRoot(root string, path string) (bool, error) {
	rootAbs, err := filepath.Abs(strings.TrimSpace(root))
	if err != nil {
		return false, err
	}
	pathAbs, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		return false, err
	}
	rel, err := filepath.Rel(rootAbs, pathAbs)
	if err != nil {
		return false, err
	}
	rel = filepath.Clean(rel)
	if rel == "." {
		return true, nil
	}
	return !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "..", nil
}
