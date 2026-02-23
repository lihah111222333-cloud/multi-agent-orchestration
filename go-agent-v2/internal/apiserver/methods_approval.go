// methods_approval.go — 审批回复 JSON-RPC 方法。
package apiserver

import "context"

type approvalRespondParams struct {
	RequestID int64 `json:"requestId"`
	Approved  bool  `json:"approved"`
}

func (s *Server) approvalRespondTyped(_ context.Context, p approvalRespondParams) (any, error) {
	if p.RequestID <= 0 {
		return map[string]any{
			"ok":     false,
			"status": "invalid_request_id",
		}, nil
	}

	if !s.ResolvePendingRequest(p.RequestID, map[string]any{"approved": p.Approved}) {
		return map[string]any{
			"ok":     false,
			"status": "not_pending",
		}, nil
	}

	return map[string]any{
		"ok":     true,
		"status": "resolved",
	}, nil
}
