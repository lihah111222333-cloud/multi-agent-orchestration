package apiserver

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multi-agent/go-agent-v2/internal/service"
	"github.com/multi-agent/go-agent-v2/internal/store"
	"github.com/multi-agent/go-agent-v2/internal/uistate"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

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

	// Dashboard stores
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
