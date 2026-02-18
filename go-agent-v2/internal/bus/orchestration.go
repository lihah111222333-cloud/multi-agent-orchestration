// orchestration.go — 编排状态跟踪器 (对应 Python orchestration_tui_bus.py)。
//
// 跟踪 active runs，发布 Begin/Update/End 事件到 MessageBus。
// 与 Python 版不同: 不使用 JSON 文件锁，而是内存 + 总线事件。
package bus

import (
	"encoding/json"
	"sync"
	"time"
)

// RunState 单个编排任务运行的状态。
type RunState struct {
	RunID         string    `json:"run_id"`
	StatusHeader  string    `json:"status_header"`
	StatusDetails string    `json:"status_details"`
	LastSeq       int64     `json:"last_seq"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// OrchestrationSnapshot 编排状态快照。
type OrchestrationSnapshot struct {
	Seq            int64      `json:"seq"`
	UpdatedAt      time.Time  `json:"updated_at"`
	Running        bool       `json:"running"`
	ActiveCount    int        `json:"active_count"`
	BindingWarning string     `json:"binding_warning,omitempty"`
	ActiveRuns     []RunState `json:"active_runs"`
}

// OrchestrationState 编排状态跟踪器。
type OrchestrationState struct {
	mu             sync.RWMutex // 保护 activeRuns/bindingWarning
	activeRuns     map[string]*RunState
	bindingWarning string
	bus            *MessageBus
}

// NewOrchestrationState 创建编排状态跟踪器。
func NewOrchestrationState(bus *MessageBus) *OrchestrationState {
	return &OrchestrationState{
		activeRuns: make(map[string]*RunState),
		bus:        bus,
	}
}

// BeginRun 开始一个编排任务运行。
func (o *OrchestrationState) BeginRun(runID, statusHeader, statusDetails, source string) {
	o.mu.Lock()
	now := time.Now()
	run := &RunState{
		RunID:         runID,
		StatusHeader:  statusHeader,
		StatusDetails: statusDetails,
		UpdatedAt:     now,
	}
	o.activeRuns[runID] = run
	o.mu.Unlock()

	o.publishEvent("BeginOrchestrationTaskState", runID, source, map[string]string{
		"status_header":  statusHeader,
		"status_details": statusDetails,
	})
}

// UpdateRun 更新编排任务状态。
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

// EndRun 结束编排任务运行。
func (o *OrchestrationState) EndRun(runID, source string) {
	o.mu.Lock()
	delete(o.activeRuns, runID)
	o.mu.Unlock()

	o.publishEvent("EndOrchestrationTaskState", runID, source, nil)
}

// SetBindingWarning 设置绑定警告。
func (o *OrchestrationState) SetBindingWarning(warning, source string) {
	o.mu.Lock()
	o.bindingWarning = warning
	o.mu.Unlock()

	o.publishEvent("SetOrchestrationBindingWarning", "", source, map[string]string{
		"warning": warning,
	})
}

// Snapshot 返回当前编排状态快照。
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

// Reset 重置编排状态。
func (o *OrchestrationState) Reset(source string) {
	o.mu.Lock()
	o.activeRuns = make(map[string]*RunState)
	o.bindingWarning = ""
	o.mu.Unlock()

	o.publishEvent("ResetOrchestrationState", "", source, nil)
}

// publishEvent 发布编排事件到总线。
func (o *OrchestrationState) publishEvent(event, runID, source string, extra map[string]string) {
	payload := map[string]string{
		"event":  event,
		"run_id": runID,
	}
	for k, v := range extra {
		payload[k] = v
	}
	data, _ := json.Marshal(payload)

	o.bus.Publish(Message{
		Topic:   TopicOrchestration + "." + event,
		From:    source,
		To:      TopicAll,
		Type:    MsgOrchestration,
		Payload: data,
	})
}
