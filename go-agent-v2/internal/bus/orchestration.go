package bus

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

type RunState struct {
	RunID         string    `json:"run_id"`
	StatusHeader  string    `json:"status_header"`
	StatusDetails string    `json:"status_details"`
	LastSeq       int64     `json:"last_seq"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type OrchestrationSnapshot struct {
	Seq            int64      `json:"seq"`
	UpdatedAt      time.Time  `json:"updated_at"`
	Running        bool       `json:"running"`
	ActiveCount    int        `json:"active_count"`
	BindingWarning string     `json:"binding_warning,omitempty"`
	ActiveRuns     []RunState `json:"active_runs"`
}

type OrchestrationState struct {
	mu             sync.RWMutex
	activeRuns     map[string]*RunState
	bindingWarning string
	bus            *MessageBus
}

func NewOrchestrationState(bus *MessageBus) *OrchestrationState {
	return &OrchestrationState{activeRuns: make(map[string]*RunState), bus: bus}
}

func (o *OrchestrationState) BeginRun(runID, statusHeader, statusDetails, source string) {
	o.mu.Lock()
	o.activeRuns[runID] = &RunState{
		RunID:         runID,
		StatusHeader:  statusHeader,
		StatusDetails: statusDetails,
		UpdatedAt:     time.Now(),
	}
	o.mu.Unlock()

	o.publishEvent("BeginOrchestrationTaskState", runID, source, map[string]string{
		"status_header":  statusHeader,
		"status_details": statusDetails,
	})
}

func (o *OrchestrationState) UpdateRun(runID, statusHeader, statusDetails, source string) {
	o.mu.Lock()
	run, ok := o.activeRuns[runID]
	if !ok {
		run = &RunState{RunID: runID}
		o.activeRuns[runID] = run
	}
	if statusHeader != "" {
		run.StatusHeader = statusHeader
	}
	if statusDetails != "" {
		run.StatusDetails = statusDetails
	}
	run.UpdatedAt = time.Now()
	o.mu.Unlock()

	o.publishEvent("UpdateOrchestrationTaskState", runID, source, map[string]string{
		"status_header":  statusHeader,
		"status_details": statusDetails,
	})
}

func (o *OrchestrationState) EndRun(runID, source string) {
	o.mu.Lock()
	delete(o.activeRuns, runID)
	o.mu.Unlock()

	o.publishEvent("EndOrchestrationTaskState", runID, source, nil)
}

func (o *OrchestrationState) SetBindingWarning(warning, source string) {
	o.mu.Lock()
	o.bindingWarning = warning
	o.mu.Unlock()

	o.publishEvent("SetOrchestrationBindingWarning", "", source, map[string]string{
		"warning": warning,
	})
}

func (o *OrchestrationState) Snapshot() OrchestrationSnapshot {
	o.mu.RLock()
	defer o.mu.RUnlock()

	runs := make([]RunState, 0, len(o.activeRuns))
	for _, r := range o.activeRuns {
		runs = append(runs, *r)
	}

	return OrchestrationSnapshot{
		Seq:            o.bus.Seq(),
		UpdatedAt:      time.Now(),
		Running:        len(runs) > 0,
		ActiveCount:    len(runs),
		BindingWarning: o.bindingWarning,
		ActiveRuns:     runs,
	}
}

func (o *OrchestrationState) Reset(source string) {
	o.mu.Lock()
	o.activeRuns = make(map[string]*RunState)
	o.bindingWarning = ""
	o.mu.Unlock()

	o.publishEvent("ResetOrchestrationState", "", source, nil)
}

func (o *OrchestrationState) publishEvent(event, runID, source string, extra map[string]string) {
	payload := map[string]string{"event": event, "run_id": runID}
	for k, v := range extra {
		payload[k] = v
	}
	data, err := json.Marshal(payload)
	if err != nil {
		logger.Warn("orchestration: event marshal failed",
			"event", event,
			logger.FieldError, err,
		)
		return
	}

	o.bus.Publish(Message{
		Topic:   TopicOrchestration + "." + event,
		From:    source,
		To:      TopicAll,
		Type:    MsgOrchestration,
		Payload: data,
	})
}
