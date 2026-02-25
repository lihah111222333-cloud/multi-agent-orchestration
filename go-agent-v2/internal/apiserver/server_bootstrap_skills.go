package apiserver

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/multi-agent/go-agent-v2/internal/service"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

func ensureSkillsCacheDir(path string) string {
	if err := os.MkdirAll(path, 0o755); err != nil {
		logger.Warn("skills directory: ensure local fallback failed", logger.FieldError, err, logger.FieldPath, path)
	}
	return path
}

func defaultSkillsCacheDir() string {
	localFallback := filepath.Join(".multi-agent", "skills-cache")

	homeDir, err := os.UserHomeDir()
	if err != nil {
		logger.Warn("skills directory: resolve user home failed, fallback to local path",
			logger.FieldError, err,
		)
		return ensureSkillsCacheDir(localFallback)
	}
	homeDir = strings.TrimSpace(homeDir)
	if homeDir == "" {
		logger.Warn("skills directory: user home empty, fallback to local path")
		return ensureSkillsCacheDir(localFallback)
	}

	appRootDir := filepath.Join(homeDir, ".multi-agent")
	if err := os.MkdirAll(appRootDir, 0o755); err != nil {
		logger.Warn("skills directory: ensure app root failed, fallback to local path",
			logger.FieldError, err,
			logger.FieldPath, appRootDir,
		)
		return ensureSkillsCacheDir(localFallback)
	}
	cacheDir := filepath.Join(appRootDir, "skills-cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		logger.Warn("skills directory: ensure cache dir failed, fallback to local path",
			logger.FieldError, err,
			logger.FieldPath, cacheDir,
		)
		return ensureSkillsCacheDir(localFallback)
	}
	return cacheDir
}

func initSkills(s *Server, skillsDir string) {
	if s == nil {
		return
	}

	dir := strings.TrimSpace(skillsDir)
	if dir == "" {
		dir = defaultSkillsCacheDir()
	} else if err := os.MkdirAll(dir, 0o755); err != nil {
		logger.Warn("app-server: ensure custom skills dir failed, fallback to app cache",
			logger.FieldError, err,
			logger.FieldPath, dir,
		)
		dir = defaultSkillsCacheDir()
	}

	s.skillsDir = dir
	s.skillSvc = service.NewSkillService(dir)
	s.skillsMgr = newSkillsManager(s)
}
