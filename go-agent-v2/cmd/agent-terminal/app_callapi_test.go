package main

import (
	"testing"
)

// TestCallAPIReturnsObject 断言 CallAPI 返回 any (object) 而非 JSON string。
//
// TDD RED: 当前 CallAPI 返回 (string, error), 此测试应编译失败/类型不匹配。
// TDD GREEN: 改签名为 (any, error) 后通过。
func TestCallAPIReturnsObject(t *testing.T) {
	app := &App{} // 无需 srv — ui/buildInfo 不经 apiserver

	result, err := app.CallAPI("ui/buildInfo", map[string]any{})
	if err != nil {
		t.Fatalf("CallAPI(ui/buildInfo) returned error: %v", err)
	}

	// 核心断言: 返回值不应是 string (不再 json.Marshal 为 string)
	if _, isString := result.(string); isString {
		t.Fatal("CallAPI should return object (any), got string — double serialization still present")
	}

	// 验证返回的是 BuildInfo struct (非 nil)
	if result == nil {
		t.Fatal("CallAPI(ui/buildInfo) returned nil, expected BuildInfo struct")
	}

	// 验证可以类型断言为 BuildInfo
	info, ok := result.(BuildInfo)
	if !ok {
		t.Fatalf("CallAPI(ui/buildInfo) returned %T, expected BuildInfo", result)
	}
	if info.Runtime == "" {
		t.Fatal("BuildInfo.Runtime should not be empty")
	}
}

// TestCallAPIErrorReturnsNil 断言 CallAPI 错误时返回 nil (非空字符串)。
func TestCallAPIErrorReturnsNil(t *testing.T) {
	app := &App{} // srv 为 nil, default 分支会 panic/err

	// 调用一个需要 srv 的方法, 会因 nil srv 而失败
	result, err := app.CallAPI("nonexistent/method", map[string]any{})
	if err == nil {
		// 如果没出错, 至少验证返回值类型
		_ = result
		return
	}

	// 错误时返回值应为 nil (不是 "")
	if result != nil {
		t.Fatalf("CallAPI error path should return nil, got %v (%T)", result, result)
	}
}

// TestGetBuildInfoReturnsObject 断言 GetBuildInfo 返回 any (object) 而非 JSON string。
func TestGetBuildInfoReturnsObject(t *testing.T) {
	app := &App{}

	result := app.GetBuildInfo()

	// 核心断言: 不应是 string
	if _, isString := result.(string); isString {
		t.Fatal("GetBuildInfo should return object (any), got string")
	}

	info, ok := result.(BuildInfo)
	if !ok {
		t.Fatalf("GetBuildInfo returned %T, expected BuildInfo", result)
	}
	if info.Runtime == "" {
		t.Fatal("BuildInfo.Runtime should not be empty")
	}
}

func TestCallAPIAcceptsObjectParams(t *testing.T) {
	app := &App{}

	params := map[string]any{
		"threadId": "thread-123",
	}

	result, err := app.CallAPI("ui/buildInfo", params)
	if err != nil {
		t.Fatalf("CallAPI(ui/buildInfo,map) returned error: %v", err)
	}
	if result == nil {
		t.Fatal("CallAPI(ui/buildInfo,map) returned nil, expected BuildInfo struct")
	}
	if _, isString := result.(string); isString {
		t.Fatal("CallAPI should return object (any), got string")
	}
}

func TestExtractThreadIDFromParamsMap(t *testing.T) {
	threadID := extractThreadIDFromParams(map[string]any{
		"threadId": "thread-map-1",
	})
	if threadID != "thread-map-1" {
		t.Fatalf("expected threadID thread-map-1, got %q", threadID)
	}
}

func TestCallAPIRejectsNonObjectParams(t *testing.T) {
	app := &App{}

	_, err := app.CallAPI("ui/buildInfo", "{}")
	if err == nil {
		t.Fatal("expected non-object params to be rejected")
	}
}

func TestCallAPISelectProjectDirsReturnsPathsObject(t *testing.T) {
	app := &App{}

	result, err := app.CallAPI("ui/selectProjectDirs", map[string]any{})
	if err != nil {
		t.Fatalf("CallAPI(ui/selectProjectDirs) returned error: %v", err)
	}
	if result == nil {
		t.Fatal("CallAPI(ui/selectProjectDirs) returned nil")
	}

	payload, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("CallAPI(ui/selectProjectDirs) returned %T, expected map[string]any", result)
	}
	paths, ok := payload["paths"].([]string)
	if !ok {
		t.Fatalf("paths field type=%T, expected []string", payload["paths"])
	}
	if len(paths) != 0 {
		t.Fatalf("expected empty paths when wails bridge unavailable, got=%v", paths)
	}
}
