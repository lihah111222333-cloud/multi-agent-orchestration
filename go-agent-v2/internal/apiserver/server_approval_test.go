package apiserver

import (
	"context"
	"reflect"
	"testing"
)

func boolPtr(value bool) *bool {
	return &value
}

func TestNormalizeApprovalResultPayloadCommandDecisionString(t *testing.T) {
	result := map[string]any{"decision": "accept_for_session"}
	got, ok := normalizeApprovalResultPayload(approvalMethodCommandExecution, result)
	if !ok {
		t.Fatalf("normalizeApprovalResultPayload() = not ok, want ok")
	}
	want := map[string]any{"decision": "acceptForSession"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalizeApprovalResultPayload() = %#v, want %#v", got, want)
	}
}

func TestNormalizeApprovalResultPayloadCommandAmendmentObject(t *testing.T) {
	result := map[string]any{
		"decision": map[string]any{
			"acceptWithExecpolicyAmendment": map[string]any{
				"execpolicyAmendment": []any{"allow", "deny"},
			},
		},
	}
	got, ok := normalizeApprovalResultPayload(approvalMethodCommandExecution, result)
	if !ok {
		t.Fatalf("normalizeApprovalResultPayload() = not ok, want ok")
	}
	want := map[string]any{
		"decision": map[string]any{
			"acceptWithExecpolicyAmendment": map[string]any{
				"execpolicy_amendment": []any{"allow", "deny"},
			},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalizeApprovalResultPayload() = %#v, want %#v", got, want)
	}
}

func TestNormalizeApprovalResultPayloadFileChangeDecisionAndFallback(t *testing.T) {
	result := map[string]any{"decision": "cancel"}
	got, ok := normalizeApprovalResultPayload(approvalMethodFileChange, result)
	if !ok {
		t.Fatalf("normalizeApprovalResultPayload() = not ok, want ok")
	}
	want := map[string]any{"decision": "cancel"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalizeApprovalResultPayload() = %#v, want %#v", got, want)
	}

	unsupported := map[string]any{
		"decision": map[string]any{
			"acceptWithExecpolicyAmendment": map[string]any{
				"execpolicy_amendment": []any{"allow"},
			},
		},
		"approved": true,
	}
	fallback, fallbackOK := normalizeApprovalResultPayload(approvalMethodFileChange, unsupported)
	if !fallbackOK {
		t.Fatalf("normalizeApprovalResultPayload() fallback = not ok, want ok")
	}
	fallbackWant := map[string]any{"decision": "accept"}
	if !reflect.DeepEqual(fallback, fallbackWant) {
		t.Fatalf("normalizeApprovalResultPayload() fallback = %#v, want %#v", fallback, fallbackWant)
	}
}

func TestNormalizeApprovalResultPayloadSkillDecisionAndFallback(t *testing.T) {
	decisionResult := map[string]any{"decision": "accept"}
	got, ok := normalizeApprovalResultPayload(approvalMethodSkillRequest, decisionResult)
	if !ok {
		t.Fatalf("normalizeApprovalResultPayload() = not ok, want ok")
	}
	want := map[string]any{"decision": "approve"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalizeApprovalResultPayload() = %#v, want %#v", got, want)
	}

	fallbackResult := map[string]any{"approved": true}
	fallback, fallbackOK := normalizeApprovalResultPayload(approvalMethodSkillRequest, fallbackResult)
	if !fallbackOK {
		t.Fatalf("normalizeApprovalResultPayload() fallback = not ok, want ok")
	}
	fallbackWant := map[string]any{"decision": "approve"}
	if !reflect.DeepEqual(fallback, fallbackWant) {
		t.Fatalf("normalizeApprovalResultPayload() fallback = %#v, want %#v", fallback, fallbackWant)
	}
}

func TestApprovalDecisionAllowsSubmitNetworkPolicyAction(t *testing.T) {
	allowPayload := map[string]any{
		"decision": map[string]any{
			"applyNetworkPolicyAmendment": map[string]any{
				"network_policy_amendment": map[string]any{
					"action": "allow",
					"host":   "example.com",
				},
			},
		},
	}
	if !approvalDecisionAllowsSubmit(approvalMethodCommandExecution, allowPayload) {
		t.Fatalf("approvalDecisionAllowsSubmit() = false, want true for allow action")
	}

	denyPayload := map[string]any{
		"decision": map[string]any{
			"applyNetworkPolicyAmendment": map[string]any{
				"network_policy_amendment": map[string]any{
					"action": "deny",
					"host":   "example.com",
				},
			},
		},
	}
	if approvalDecisionAllowsSubmit(approvalMethodCommandExecution, denyPayload) {
		t.Fatalf("approvalDecisionAllowsSubmit() = true, want false for deny action")
	}
}

func TestApprovalDecisionAllowsSubmitSkillDecision(t *testing.T) {
	approvePayload := map[string]any{"decision": "approve"}
	if !approvalDecisionAllowsSubmit(approvalMethodSkillRequest, approvePayload) {
		t.Fatalf("approvalDecisionAllowsSubmit() = false, want true for approve")
	}

	acceptAliasPayload := map[string]any{"decision": "accept"}
	if !approvalDecisionAllowsSubmit(approvalMethodSkillRequest, acceptAliasPayload) {
		t.Fatalf("approvalDecisionAllowsSubmit() = false, want true for accept alias")
	}

	declinePayload := map[string]any{"decision": "decline"}
	if approvalDecisionAllowsSubmit(approvalMethodSkillRequest, declinePayload) {
		t.Fatalf("approvalDecisionAllowsSubmit() = true, want false for decline")
	}
}

func TestApprovalRespondTypedSupportsDecisionPayload(t *testing.T) {
	s := &Server{}
	reqID, ch, cleanup := allocPendingRequestState(s)
	defer cleanup()

	decision := map[string]any{
		"applyNetworkPolicyAmendment": map[string]any{
			"network_policy_amendment": map[string]any{
				"action": "allow",
				"host":   "example.com",
			},
		},
	}
	result, err := approvalRespondTyped(s, context.Background(), approvalRespondParams{
		RequestID: reqID,
		Decision:  decision,
	})
	if err != nil {
		t.Fatalf("approvalRespondTyped() error = %v, want nil", err)
	}
	resultMap, ok := result.(map[string]any)
	if !ok || resultMap["ok"] != true || resultMap["status"] != "resolved" {
		t.Fatalf("approvalRespondTyped() result = %#v, want resolved ok response", result)
	}

	select {
	case pending := <-ch:
		if pending == nil {
			t.Fatalf("pending response = nil, want non-nil")
		}
		pendingMap, ok := pending.Result.(map[string]any)
		if !ok {
			t.Fatalf("pending response result type = %T, want map[string]any", pending.Result)
		}
		if !reflect.DeepEqual(pendingMap["decision"], decision) {
			t.Fatalf("pending decision = %#v, want %#v", pendingMap["decision"], decision)
		}
	default:
		t.Fatalf("pending response not delivered")
	}
}

func TestApprovalRespondTypedSupportsApprovedFallback(t *testing.T) {
	s := &Server{}
	reqID, ch, cleanup := allocPendingRequestState(s)
	defer cleanup()

	result, err := approvalRespondTyped(s, context.Background(), approvalRespondParams{
		RequestID: reqID,
		Approved:  boolPtr(true),
	})
	if err != nil {
		t.Fatalf("approvalRespondTyped() error = %v, want nil", err)
	}
	resultMap, ok := result.(map[string]any)
	if !ok || resultMap["ok"] != true || resultMap["status"] != "resolved" {
		t.Fatalf("approvalRespondTyped() result = %#v, want resolved ok response", result)
	}

	select {
	case pending := <-ch:
		if pending == nil {
			t.Fatalf("pending response = nil, want non-nil")
		}
		pendingMap, ok := pending.Result.(map[string]any)
		if !ok {
			t.Fatalf("pending response result type = %T, want map[string]any", pending.Result)
		}
		if pendingMap["approved"] != true {
			t.Fatalf("pending approved = %#v, want true", pendingMap["approved"])
		}
	default:
		t.Fatalf("pending response not delivered")
	}
}

func TestApprovalRespondTypedRequiresDecisionOrApproved(t *testing.T) {
	s := &Server{}
	result, err := approvalRespondTyped(s, context.Background(), approvalRespondParams{RequestID: 1})
	if err != nil {
		t.Fatalf("approvalRespondTyped() error = %v, want nil", err)
	}
	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("approvalRespondTyped() result type = %T, want map[string]any", result)
	}
	if resultMap["ok"] != false || resultMap["status"] != "decision_or_approved_required" {
		t.Fatalf("approvalRespondTyped() result = %#v, want decision_or_approved_required", resultMap)
	}
}
