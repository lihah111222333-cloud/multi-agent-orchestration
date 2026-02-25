package apiserver

import (
	"reflect"
	"testing"
)

func TestServerStateGroupsAreEmbedded(t *testing.T) {
	t.Parallel()

	serverType := reflect.TypeOf(Server{})
	required := []reflect.Type{
		reflect.TypeOf(connManagerState{}),
		reflect.TypeOf(diagnosticsCacheState{}),
		reflect.TypeOf(codeRunState{}),
		reflect.TypeOf(turnTrackingState{}),
		reflect.TypeOf(uiThrottleState{}),
		reflect.TypeOf(storeBundle{}),
	}
	for _, groupType := range required {
		field, ok := findTopLevelFieldByType(serverType, groupType)
		if !ok {
			t.Fatalf("Server must embed %s", groupType.Name())
		}
		if !field.Anonymous {
			t.Fatalf("Server field %s must be embedded (anonymous)", field.Name)
		}
	}
}

func TestServerLegacyStateFieldsNotAtTopLevel(t *testing.T) {
	t.Parallel()

	serverType := reflect.TypeOf(Server{})
	topFields := make(map[string]struct{}, serverType.NumField())
	for i := 0; i < serverType.NumField(); i++ {
		field := serverType.Field(i)
		topFields[field.Name] = struct{}{}
	}

	forbidden := []string{
		"mu", "conns", "nextID",
		"pendingMu", "pending", "nextReqID",
		"diagMu", "diagCache",
		"codeRunMu", "activeCodeRuns", "codeRunSeq",
		"agentWorkDirMu", "agentWorkDirs",
		"threadSeq", "fileChangeMu", "fileChangeByThread",
		"orchestrationReportMu", "orchestrationPendingReports", "orchestrationReportTTL",
		"uiThrottleMu", "uiThrottleEntries",
		"dagStore", "cmdStore", "promptStore", "fileStore", "workspaceRunStore", "sysLogStore",
		"agentStatusStore", "auditLogStore", "aiLogStore", "busLogStore", "taskAckStore", "taskTraceStore",
		"bindingStore",
	}
	for _, name := range forbidden {
		if _, exists := topFields[name]; exists {
			t.Fatalf("state field %q must live in an embedded state group, not at Server top-level", name)
		}
	}
}

func TestServerStateGroupShapes(t *testing.T) {
	t.Parallel()

	mustHaveFields(t, reflect.TypeOf(connManagerState{}), []string{
		"mu", "conns", "nextID", "pendingMu", "pending", "nextReqID",
	})
	mustHaveFields(t, reflect.TypeOf(diagnosticsCacheState{}), []string{
		"diagMu", "diagCache",
	})
	mustHaveFields(t, reflect.TypeOf(codeRunState{}), []string{
		"codeRunMu", "activeCodeRuns", "codeRunSeq", "agentWorkDirMu", "agentWorkDirs",
	})
	mustHaveFields(t, reflect.TypeOf(turnTrackingState{}), []string{
		"threadSeq", "fileChangeMu", "fileChangeByThread",
		"orchestrationReportMu", "orchestrationPendingReports", "orchestrationReportTTL",
	})
	mustHaveFields(t, reflect.TypeOf(uiThrottleState{}), []string{
		"uiThrottleMu", "uiThrottleEntries",
	})
	mustHaveFields(t, reflect.TypeOf(storeBundle{}), []string{
		"dagStore", "cmdStore", "promptStore", "fileStore", "workspaceRunStore", "sysLogStore",
		"agentStatusStore", "auditLogStore", "aiLogStore", "busLogStore", "taskAckStore", "taskTraceStore",
		"bindingStore",
	})
}

func findTopLevelFieldByType(t reflect.Type, target reflect.Type) (reflect.StructField, bool) {
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.Type == target {
			return field, true
		}
	}
	return reflect.StructField{}, false
}

func mustHaveFields(t *testing.T, st reflect.Type, required []string) {
	t.Helper()
	for _, name := range required {
		if _, ok := st.FieldByName(name); !ok {
			t.Fatalf("%s must contain field %q", st.Name(), name)
		}
	}
}
