package codexadapter

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
)

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
