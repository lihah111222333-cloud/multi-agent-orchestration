package archive

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/codex"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
)

func resolveUniquePath(op, exhaustedMsg, statMsg, primary string, next func(index int) string) (string, error) {
	for i := 1; i <= 9999; i++ {
		candidate := primary
		if i > 1 {
			candidate = next(i)
		}
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate, nil
		} else if err != nil {
			return "", apperrors.Wrap(err, op, statMsg)
		}
	}
	return "", apperrors.New(op, exhaustedMsg)
}

func resolveUserHome(op string) (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", apperrors.Wrap(err, op, "resolve user home")
	}
	if homeDir = strings.TrimSpace(homeDir); homeDir == "" {
		return "", apperrors.New(op, "user home is empty")
	}
	return homeDir, nil
}

func ResolveThreadArchiveRootDir() (string, error) {
	homeDir, err := resolveUserHome("ResolveThreadArchiveRootDir")
	if err != nil {
		return "", err
	}
	appRootDir := filepath.Join(homeDir, ".multi-agent")
	if err := os.MkdirAll(appRootDir, 0o755); err != nil {
		return "", apperrors.Wrap(err, "ResolveThreadArchiveRootDir", "ensure app root")
	}
	archiveRoot := filepath.Join(appRootDir, "thread-archives")
	if err := os.MkdirAll(archiveRoot, 0o755); err != nil {
		return "", apperrors.Wrap(err, "ResolveThreadArchiveRootDir", "ensure archive root")
	}
	return archiveRoot, nil
}

func ResolveThreadArchiveSnapshotDir(rootDir string, threadID string, archivedAt string) (string, error) {
	safeThreadID, err := SanitizeArchiveNameStrict(strings.TrimSpace(threadID))
	if err != nil {
		return "", apperrors.Wrap(err, "ResolveThreadArchiveSnapshotDir", "sanitize thread id")
	}
	threadDir := filepath.Join(rootDir, safeThreadID)
	if err := os.MkdirAll(threadDir, 0o755); err != nil {
		return "", apperrors.Wrap(err, "ResolveThreadArchiveSnapshotDir", "ensure thread dir")
	}
	snapshotName, err := SanitizeArchiveNameStrict(strings.TrimSpace(archivedAt))
	if err != nil {
		return "", apperrors.Wrap(err, "ResolveThreadArchiveSnapshotDir", "sanitize archive timestamp")
	}
	return resolveUniquePath(
		"ResolveThreadArchiveSnapshotDir",
		"unable to allocate unique archive snapshot dir",
		"stat snapshot candidate",
		filepath.Join(threadDir, snapshotName),
		func(i int) string { return filepath.Join(threadDir, fmt.Sprintf("%s-%d", snapshotName, i)) },
	)
}

func CollectThreadArtifactCandidates(codexThreadID string, rolloutPath string) []ThreadArtifactCandidate {
	candidates := make([]ThreadArtifactCandidate, 0, 8)
	seen := make(map[string]struct{}, 8)
	addCandidate := func(kind, path string) {
		if path = strings.TrimSpace(path); path == "" {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		candidates = append(candidates, ThreadArtifactCandidate{Kind: kind, Path: path})
	}

	codexThreadID = strings.TrimSpace(codexThreadID)
	resolvedRollout := strings.TrimSpace(rolloutPath)
	if resolvedRollout == "" && codexThreadID != "" {
		if found, err := codex.FindRolloutPath(codexThreadID); err == nil {
			resolvedRollout = strings.TrimSpace(found)
		}
	}
	addCandidate("rollout", resolvedRollout)
	if codexThreadID == "" {
		return candidates
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return candidates
	}
	addCandidate("shell_snapshot", filepath.Join(homeDir, ".codex", "shell_snapshots", codexThreadID+".sh"))

	for _, name := range []string{"sessions", "shell_snapshots", "archived_sessions", "tmp"} {
		root := filepath.Join(homeDir, ".codex", name)
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr == nil && d != nil && !d.IsDir() && strings.Contains(d.Name(), codexThreadID) {
				addCandidate(InferThreadArtifactKind(d.Name()), path)
			}
			return nil
		})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Kind != candidates[j].Kind {
			return candidates[i].Kind < candidates[j].Kind
		}
		return candidates[i].Path < candidates[j].Path
	})
	return candidates
}

func NextArchiveFilePath(dir, filename string) (string, error) {
	base, err := SanitizeArchiveNameStrict(filepath.Base(filename))
	if err != nil {
		return "", apperrors.Wrap(err, "NextArchiveFilePath", "sanitize filename")
	}
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	return resolveUniquePath(
		"NextArchiveFilePath",
		"unable to allocate unique archive filename",
		"stat archive target candidate",
		filepath.Join(dir, base),
		func(i int) string { return filepath.Join(dir, fmt.Sprintf("%s-%d%s", stem, i, ext)) },
	)
}

func copyFileAtomic(srcPath, targetPath string, overwrite bool) error {
	op := "CopyFile"
	if overwrite {
		op = "CopyFileOverwrite"
	}
	if !overwrite {
		if _, err := os.Stat(targetPath); err == nil {
			return apperrors.Newf(op, "target already exists: %s", targetPath)
		} else if !os.IsNotExist(err) {
			return apperrors.Wrap(err, op, "stat target path")
		}
	}
	info, err := os.Stat(srcPath)
	if err != nil {
		return err
	}
	mode := info.Mode().Perm()
	if mode == 0 {
		mode = 0o644
	}
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return err
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
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.WriteFile(tmpPath, data, mode); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, targetPath); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

func CopyFile(srcPath, targetPath string) error { return copyFileAtomic(srcPath, targetPath, false) }

func CopyFileOverwrite(srcPath, targetPath string) error {
	return copyFileAtomic(srcPath, targetPath, true)
}

func FileSHA256(path string) (string, error) {
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

func FindLatestThreadArchiveManifestPath(threadDir string) (string, bool, error) {
	info, err := os.Stat(threadDir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	if !info.IsDir() {
		return "", false, apperrors.Newf("FindLatestThreadArchiveManifestPath", "thread archive path is not a directory: %s", threadDir)
	}
	entries, err := os.ReadDir(threadDir)
	if err != nil {
		return "", false, apperrors.Wrap(err, "FindLatestThreadArchiveManifestPath", "read thread archive dir")
	}

	var latestPath string
	var latestAt int64
	pick := func(path, action string) error {
		stat, statErr := os.Stat(path)
		if statErr != nil {
			if os.IsNotExist(statErr) {
				return nil
			}
			return apperrors.Wrap(statErr, "FindLatestThreadArchiveManifestPath", action)
		}
		if !stat.IsDir() {
			modAt := stat.ModTime().UnixNano()
			if latestPath == "" || modAt > latestAt || (modAt == latestAt && path > latestPath) {
				latestPath, latestAt = path, modAt
			}
		}
		return nil
	}
	if err := pick(filepath.Join(threadDir, "manifest.json"), "stat legacy manifest"); err != nil {
		return "", false, err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			if err := pick(filepath.Join(threadDir, entry.Name(), "manifest.json"), fmt.Sprintf("stat manifest for snapshot %s", entry.Name())); err != nil {
				return "", false, err
			}
		}
	}
	return latestPath, latestPath != "", nil
}

func ReadThreadArchiveManifest(manifestPath string) (ThreadArchiveManifest, error) {
	manifest := ThreadArchiveManifest{}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return manifest, err
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return manifest, err
	}
	return manifest, nil
}

func WriteThreadArchiveManifest(manifest ThreadArchiveManifest) error {
	if strings.TrimSpace(manifest.ArchiveDir) == "" {
		return apperrors.New("WriteThreadArchiveManifest", "archive dir is empty")
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(manifest.ArchiveDir, "manifest.json"), data, 0o644)
}

func ResolveCodexRootDir() (string, error) {
	homeDir, err := resolveUserHome("ResolveCodexRootDir")
	if err != nil {
		return "", err
	}
	return filepath.Join(homeDir, ".codex"), nil
}

func FileState(path string) (ThreadArchiveFileState, error) {
	state := ThreadArchiveFileState{}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return state, nil
		}
		return state, err
	}
	state.Exists, state.IsDir, state.SizeBytes = true, info.IsDir(), info.Size()
	return state, nil
}

func RemoveFile(path string) error { return os.Remove(path) }

func RemoveEmptyCodexParentDirs(startDir string, codexRoot string) {
	current, root := strings.TrimSpace(startDir), strings.TrimSpace(codexRoot)
	if current == "" || root == "" {
		return
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return
	}
	for current != "" {
		currentAbs, err := filepath.Abs(current)
		if err != nil || currentAbs == rootAbs {
			return
		}
		withinRoot, err := PathWithinRoot(rootAbs, currentAbs)
		if err != nil || !withinRoot {
			return
		}
		entries, err := os.ReadDir(currentAbs)
		if err != nil || len(entries) != 0 {
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

func LoadThreadArchiveMapFromDisk() (map[string]int64, error) {
	rootDir, err := ResolveThreadArchiveRootDir()
	if err != nil {
		return nil, err
	}
	return collectThreadArchiveMapFromRoot(rootDir)
}

func collectThreadArchiveMapFromRoot(rootDir string) (map[string]int64, error) {
	result := map[string]int64{}
	root := strings.TrimSpace(rootDir)
	if root == "" {
		return result, nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return result, nil
		}
		return nil, err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		threadID := strings.TrimSpace(entry.Name())
		if threadID == "" {
			continue
		}
		threadDir := filepath.Join(root, entry.Name())
		archivedAt := int64(0)
		if snapshots, err := os.ReadDir(threadDir); err == nil {
			for _, snapshot := range snapshots {
				if snapshot.IsDir() {
					archivedAt = max(archivedAt, ParseArchiveTimestamp(snapshot.Name()))
				}
			}
		}
		if manifestPath, found, manifestErr := FindLatestThreadArchiveManifestPath(threadDir); manifestErr == nil && found {
			if manifest, readErr := ReadThreadArchiveManifest(manifestPath); readErr == nil {
				if id := strings.TrimSpace(manifest.ThreadID); id != "" {
					threadID = id
				}
				archivedAt = max(archivedAt, ParseArchiveTimestamp(manifest.ArchivedAt))
			}
		}
		if archivedAt > result[threadID] {
			result[threadID] = archivedAt
		}
	}
	return result, nil
}
