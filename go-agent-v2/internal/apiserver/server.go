package apiserver

import (
	"context"
	"encoding/json"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multi-agent/go-agent-v2/internal/apiserver/codexadapter"
	"github.com/multi-agent/go-agent-v2/internal/config"
	"github.com/multi-agent/go-agent-v2/internal/executor"
	"github.com/multi-agent/go-agent-v2/internal/runner"
	"github.com/multi-agent/go-agent-v2/internal/service"
	skillsruntime "github.com/multi-agent/go-agent-v2/internal/skills"
	"github.com/multi-agent/go-agent-v2/internal/uistate"
	"github.com/multi-agent/go-agent-v2/pkg/toolsdk/lsp"
	"github.com/multi-agent/go-agent-v2/pkg/toolsdk/tooladapter"
	"github.com/multi-agent/go-agent-v2/pkg/toolsdk/tools"
)

type Handler func(ctx context.Context, params json.RawMessage) (any, error)

const (
	maxConnections    = 100
	maxMessageSize    = 4 << 20
	connOutboxSize    = 256
	connBacklogCut    = 256 - 16
	uiStateThrottleMs = 500
)

type Server struct {
	mgr                      *runner.AgentManager
	lsp                      *lsp.Manager
	lspTools                 tools.LSPProvider
	cfg                      *config.Config
	codeRunner               *executor.CodeRunner
	codexAdapter             *codexadapter.Adapter
	methods                  map[string]Handler
	dynTools                 map[string]tooladapter.RuntimeToolHandler
	submitAgentMessage       func(agentID, prompt string, images, files []string) error
	lspDiagnosticsQueryTyped func(ctx context.Context, p lspDiagnosticsQueryParams) (any, error)
	storeBundle
	skillSvc     *service.SkillService
	skillsMgr    *skillsruntime.Manager
	skillsDir    string
	workspaceMgr *service.WorkspaceManager
	prefManager  *uistate.PreferenceManager
	uiRuntime    *uistate.RuntimeManager
	threadAliasState
	connManagerState
	diagnosticsCacheState
	toolCallState
	codeRunState
	turnTrackingState
	sseState
	notifyHookState
	uiThrottleState
	runtimeGuardState
	upgrader websocket.Upgrader
}

type Deps struct {
	Manager   *runner.AgentManager
	LSP       *lsp.Manager
	Config    *config.Config
	DB        *pgxpool.Pool
	SkillsDir string
}

func New(deps Deps) *Server {
	s := &Server{
		mgr:      deps.Manager,
		lsp:      deps.LSP,
		cfg:      deps.Config,
		methods:  make(map[string]Handler),
		dynTools: make(map[string]tooladapter.RuntimeToolHandler),
		connManagerState: connManagerState{
			conns:   make(map[string]*connEntry),
			pending: make(map[int64]chan *Response),
		},
		diagnosticsCacheState: diagnosticsCacheState{
			diagCache: make(map[string][]lsp.Diagnostic),
		},
		codeRunState: codeRunState{
			activeCodeRuns: make(map[string]map[string]context.CancelFunc),
			agentWorkDirs:  make(map[string]string),
		},
		turnTrackingState: turnTrackingState{
			fileChangeByThread:          make(map[string][]string),
			orchestrationPendingReports: make(map[string]map[string]time.Time),
			orchestrationReportTTL:      defaultOrchestrationReportTTL,
		},
		toolCallState: toolCallState{
			toolCallCount: make(map[string]int64),
		},
		sseState: sseState{
			clients: safeSet[chan []byte]{
				items: make(map[chan []byte]struct{}),
			},
		},
		prefManager: uistate.NewPreferenceManager(nil),
		uiRuntime:   uistate.NewRuntimeManager(),
		uiThrottleState: uiThrottleState{
			uiThrottleEntries: make(map[string]*uiStateThrottleEntry),
		},
		upgrader: websocket.Upgrader{
			CheckOrigin: checkLocalOrigin,
		},
	}
	initStores(s, deps.DB)
	initRuntimeWiring(s)
	initSkills(s, deps.SkillsDir)
	s.registerMethods()

	applyStallConfig(s, deps.Config)
	initCodeRunner(s)

	applyInjectedPromptVisibilityPreference(s, context.Background())
	registerDynamicTools(s)
	return s
}
