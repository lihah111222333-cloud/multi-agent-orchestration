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
	"github.com/multi-agent/go-agent-v2/internal/service"
	"github.com/multi-agent/go-agent-v2/internal/store"
	"github.com/multi-agent/go-agent-v2/internal/uistate"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/multi-agent/go-agent-v2/pkg/toolsdk/lsp"
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
	if s == nil || cfg == nil || s.codexAdapter == nil {
		return
	}
	if cfg.StallThresholdSec > 0 {
		threshold := time.Duration(cfg.StallThresholdSec) * time.Second
		s.codexAdapter.SetStallThreshold(threshold)
		s.codexAdapter.SetStreamReadIdleTimeout(threshold)
	}
	if cfg.StallHeartbeatSec > 0 {
		s.codexAdapter.SetStallHeartbeat(time.Duration(cfg.StallHeartbeatSec) * time.Second)
	}
}

func initCodeRunner(s *Server) {
	if s == nil {
		return
	}
	workDir, err := os.Getwd()
	if err != nil {
		logger.Warn("app-server: resolve working directory failed; fallback to current path", logger.FieldError, err)
		workDir = "."
	}
	cr, err := executor.NewCodeRunner(workDir)
	if err != nil {
		logger.Warn("app-server: code runner unavailable", logger.FieldError, err)
		return
	}
	s.codeRunner = cr
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
	s.agentThreadStore = store.NewAgentThreadStore(db)

	if s.cfg != nil {
		workspaceMgr, mgrErr := service.NewWorkspaceManager(
			s.workspaceRunStore,
			s.cfg.OrchestrationWorkspaceRoot,
			s.cfg.OrchestrationWorkspaceMaxFiles,
			int64(s.cfg.OrchestrationWorkspaceMaxFileBytes),
			int64(s.cfg.OrchestrationWorkspaceMaxTotalBytes),
		)
		if mgrErr != nil {
			logger.Warn("app-server: workspace manager unavailable", logger.FieldError, mgrErr)
		} else {
			s.workspaceMgr = workspaceMgr
			logger.Info("app-server: workspace manager enabled", logger.FieldRoot, workspaceMgr.RootDir())
		}
	}
	logger.Info("app-server: resource tools + dashboard enabled")
	recoverSubAgents(s)
}

func recoverSubAgents(s *Server) {
	if s == nil || s.agentThreadStore == nil || s.mgr == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	agents, err := s.agentThreadStore.ListRunningFull(ctx)
	if err != nil {
		logger.Warn("orchestration: recover sub-agents query failed", logger.FieldError, err)
		return
	}
	if len(agents) == 0 {
		return
	}
	logger.Info("orchestration: recovering sub-agents", "count", len(agents))
	for _, a := range agents {
		launchCtx, launchCancel := context.WithTimeout(context.Background(), 30*time.Second)
		launchErr := s.mgr.Launch(launchCtx, a.ThreadID, a.Prompt, "", a.Cwd, "", nil)
		launchCancel()
		if launchErr != nil {
			logger.Warn("orchestration: recover sub-agent failed",
				logger.FieldAgentID, a.ThreadID, logger.FieldError, launchErr)
			_ = s.agentThreadStore.Delete(context.Background(), a.ThreadID)
			continue
		}
		logger.Info("orchestration: sub-agent recovered", logger.FieldAgentID, a.ThreadID, logger.FieldName, a.Prompt)
	}
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

	cacheDir := filepath.Join(homeDir, ".multi-agent", "skills-cache")
	err = os.MkdirAll(cacheDir, 0o755)
	if err == nil {
		return cacheDir
	}
	logger.Warn("skills directory: ensure cache dir failed, fallback to local path",
		logger.FieldError, err,
		logger.FieldPath, cacheDir,
	)
	return ensureSkillsCacheDir(localFallback)
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
