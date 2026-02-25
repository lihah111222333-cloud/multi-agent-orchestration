package codexadapter

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"

	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
)

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
