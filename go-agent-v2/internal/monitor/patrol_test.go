package monitor

import (
	"os"
	"strings"
	"testing"
)

// ========================================
// Fix 5: patrol.go L157 应使用 Warnw 而非 Debugw
// ========================================

// TestPatrolUpsertFail_UsesWarnLevel 通过源码断言验证日志级别。
//
// Patrol.RunOnce 依赖 DB, 无法在单测中直接执行。
// 因此用源码级别检查确认 upsert 失败时使用 Warn (不是 Debug)。
func TestPatrolUpsertFail_UsesWarnLevel(t *testing.T) {
	src, err := os.ReadFile("patrol.go")
	if err != nil {
		t.Fatalf("read patrol.go: %v", err)
	}
	content := string(src)

	// 核心断言: upsert failed 应该用 Warnw, 不是 Debugw
	if strings.Contains(content, `Debugw("patrol: upsert failed"`) {
		t.Fatal("patrol.go: upsert failed should use logger.Warnw, not logger.Debugw")
	}
	if !strings.Contains(content, `Warnw("patrol: upsert failed"`) {
		t.Fatal("patrol.go: expected logger.Warnw(\"patrol: upsert failed\", ...)")
	}
}
