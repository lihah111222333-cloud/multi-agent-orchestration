package difftracker

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/pmezard/go-difflib/difflib"
)

type FileContentSnapshot struct {
	Exists  bool
	Content string
}

func CaptureWorkingTreeFileSnapshots(repoRoot string, paths []string) map[string]FileContentSnapshot {
	snapshots := make(map[string]FileContentSnapshot, len(paths))
	for _, path := range UniqueSortedPaths(paths) {
		snapshots[path] = ReadWorkingTreeFileSnapshot(repoRoot, path)
	}
	return snapshots
}

func ReadWorkingTreeFileSnapshot(repoRoot, relativePath string) FileContentSnapshot {
	clean := filepath.FromSlash(strings.TrimSpace(relativePath))
	if clean == "" {
		return FileContentSnapshot{}
	}
	absPath := filepath.Join(repoRoot, clean)
	content, err := os.ReadFile(absPath)
	if err != nil {
		return FileContentSnapshot{}
	}
	return FileContentSnapshot{Exists: true, Content: string(content)}
}

func ReadHeadFileSnapshot(repoRoot, relativePath string) FileContentSnapshot {
	path := filepath.ToSlash(strings.TrimSpace(relativePath))
	if path == "" {
		return FileContentSnapshot{}
	}
	output, err := RunGit(repoRoot, "show", "HEAD:"+path)
	if err != nil {
		return FileContentSnapshot{}
	}
	return FileContentSnapshot{Exists: true, Content: output}
}

func BuildIncrementalDiffText(
	repoRoot string,
	beforeFileSnapshots map[string]FileContentSnapshot,
	afterPaths []string,
) (string, error) {
	paths := UniqueSortedPaths(afterPaths)
	if len(paths) == 0 {
		return "", nil
	}
	blocks := make([]string, 0, len(paths))
	for _, path := range paths {
		beforeSnapshot, ok := beforeFileSnapshots[path]
		if !ok {
			beforeSnapshot = ReadHeadFileSnapshot(repoRoot, path)
		}
		afterSnapshot := ReadWorkingTreeFileSnapshot(repoRoot, path)
		block, err := BuildUnifiedDiffBlock(path, beforeSnapshot, afterSnapshot)
		if err != nil {
			return "", err
		}
		if block = strings.TrimSpace(block); block != "" { blocks = append(blocks, block) }
	}
	return strings.Join(blocks, "\n"), nil
}

func BuildUnifiedDiffBlock(path string, before, after FileContentSnapshot) (string, error) {
	if before.Exists == after.Exists && before.Content == after.Content {
		return "", nil
	}

	labelPath := filepath.ToSlash(strings.TrimSpace(path))
	if labelPath == "" {
		return "", nil
	}
	fromPath := "a/" + labelPath
	if !before.Exists {
		fromPath = "/dev/null"
	}
	toPath := "b/" + labelPath
	if !after.Exists {
		toPath = "/dev/null"
	}

	patchText, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        difflib.SplitLines(before.Content),
		B:        difflib.SplitLines(after.Content),
		FromFile: fromPath,
		ToFile:   toPath,
		Context:  3,
	})
	if err != nil {
		return "", err
	}
	if patchText = strings.TrimSpace(patchText); patchText == "" { return "", nil }
	return patchText + "\n", nil
}
