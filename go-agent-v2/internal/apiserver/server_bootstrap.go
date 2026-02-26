package apiserver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	appconfig "github.com/multi-agent/go-agent-v2/internal/config"
	"github.com/multi-agent/go-agent-v2/internal/executor"
	"github.com/multi-agent/go-agent-v2/pkg/toolsdk/lsp"
	"github.com/multi-agent/go-agent-v2/internal/service"
	"github.com/multi-agent/go-agent-v2/internal/store"
	"github.com/multi-agent/go-agent-v2/internal/uistate"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

func initRuntimeWiring(s *Server) {
	if s == nil {
		return
	}
	if s.mgr != nil {
		s.submitAgentMessage = s.mgr.Submit
	}
	s.codexAdapter = newCodexAdapter(s)
	s.lspTools = lsp.NewToolHandlers(s.lsp, diagnosticsAccessor(s))
	s.lspDiagnosticsQueryTyped = func(_ context.Context, p lspDiagnosticsQueryParams) (any, error) {
		return s.lspTools.DiagnosticsQuery(p.FilePath), nil
	}
}

func applyStallConfig(s *Server, cfg *appconfig.Config) {
	if s == nil || cfg == nil {
		return
	}
	if cfg.StallThresholdSec > 0 {
		threshold := time.Duration(cfg.StallThresholdSec) * time.Second
		if s.codexAdapter != nil {
			s.codexAdapter.SetStallThreshold(threshold)
			s.codexAdapter.SetStreamReadIdleTimeout(threshold)
		}
	}
	if s.codexAdapter != nil && cfg.StallHeartbeatSec > 0 {
		s.codexAdapter.SetStallHeartbeat(time.Duration(cfg.StallHeartbeatSec) * time.Second)
	}
}

func initCodeRunner(s *Server) {
	if s == nil {
		return
	}

	workDir, wdErr := os.Getwd()
	if wdErr != nil {
		logger.Warn("app-server: resolve working directory failed; fallback to current path", logger.FieldError, wdErr)
		workDir = "."
	}
	if cr, crErr := executor.NewCodeRunner(workDir); crErr != nil {
		logger.Warn("app-server: code runner unavailable", logger.FieldError, crErr)
	} else {
		s.codeRunner = cr
	}
}
func initStores(s *Server, db *pgxpool.Pool) {
	if s == nil || db == nil {
		return
	}

	s.prefManager = uistate.NewPreferenceManager(store.NewUIPreferenceStore(db))
	s.dagStore = store.NewTaskDAGStore(db)
	s.cmdStore = store.NewCommandCardStore(db)
	s.promptStore = store.NewPromptTemplateStore(db)
	s.fileStore = store.NewSharedFileStore(db)
	s.workspaceRunStore = store.NewWorkspaceRunStore(db)
	s.sysLogStore = store.NewSystemLogStore(db)

	s.agentStatusStore = store.NewAgentStatusStore(db)
	s.auditLogStore = store.NewAuditLogStore(db)
	s.aiLogStore = store.NewAILogStore(db)
	s.busLogStore = store.NewBusLogStore(db)
	s.taskAckStore = store.NewTaskAckStore(db)
	s.taskTraceStore = store.NewTaskTraceStore(db)
	s.bindingStore = store.NewAgentCodexBindingStore(db)

	if s.cfg != nil {
		maxFileBytes := int64(s.cfg.OrchestrationWorkspaceMaxFileBytes)
		maxTotalBytes := int64(s.cfg.OrchestrationWorkspaceMaxTotalBytes)
		workspaceMgr, mgrErr := service.NewWorkspaceManager(
			s.workspaceRunStore,
			s.cfg.OrchestrationWorkspaceRoot,
			s.cfg.OrchestrationWorkspaceMaxFiles,
			maxFileBytes,
			maxTotalBytes,
		)
		if mgrErr != nil {
			logger.Warn("app-server: workspace manager unavailable", logger.FieldError, mgrErr)
		} else {
			s.workspaceMgr = workspaceMgr
			logger.Info("app-server: workspace manager enabled", logger.FieldRoot, workspaceMgr.RootDir())
		}
	}
	logger.Info("app-server: resource tools + dashboard enabled")
}
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
