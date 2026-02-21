package runner

import (
	"bytes"
	"testing"
)

// ========================================
// P1-3: RingBuffer.Write 超限后复用底层数组
// ========================================

// TestRingBuffer_WriteWithinLimit 验证未超限时数据完整保留。
func TestRingBuffer_WriteWithinLimit(t *testing.T) {
	rb := NewRingBuffer(10) // 10 * 80 = 800 字节
	rb.Write([]byte("hello"))
	got := rb.String()
	if got != "hello" {
		t.Errorf("Bytes() = %q, want %q", got, "hello")
	}
}

// TestRingBuffer_WriteBeyondLimit 验证超限后旧数据被丢弃, 保留最近的 limit 字节。
func TestRingBuffer_WriteBeyondLimit(t *testing.T) {
	rb := &RingBuffer{
		data:  make([]byte, 0, 16),
		limit: 10,
	}

	rb.Write([]byte("12345678")) // 8 bytes, 未超限
	if rb.String() != "12345678" {
		t.Fatalf("before overflow: got %q", rb.String())
	}

	rb.Write([]byte("ABCDE")) // 总共 13, 超限 (limit=10), 丢弃前 3 字节
	got := rb.String()
	want := "45678ABCDE" // 13 - 10 = 3 bytes dropped
	if got != want {
		t.Errorf("after overflow: got %q, want %q", got, want)
	}
}

// TestRingBuffer_WriteOverflow_ReusesCapacity 验证超限 Write 通过 copy 复用底层 array,
// 不分配新的 []byte (P1-3 优化目标)。
func TestRingBuffer_WriteOverflow_ReusesCapacity(t *testing.T) {
	rb := &RingBuffer{
		data:  make([]byte, 0, 32),
		limit: 10,
	}

	// 第一次写满
	rb.Write([]byte("1234567890"))
	if len(rb.data) != 10 {
		t.Fatalf("expected len=10, got %d", len(rb.data))
	}

	// 记录底层 array 指针
	capBefore := cap(rb.data)

	// 追加超限写入
	rb.Write([]byte("AB"))

	// 优化后: cap 应保持不变 (复用底层 array)
	capAfter := cap(rb.data)
	if capAfter != capBefore {
		t.Errorf("cap changed from %d to %d — not reusing underlying array", capBefore, capAfter)
	}

	// 数据正确性: 丢弃最旧 2 字节
	got := rb.String()
	want := "34567890AB"
	if got != want {
		t.Errorf("data = %q, want %q", got, want)
	}
}

// TestRingBuffer_Reset 验证 Reset 清空数据。
func TestRingBuffer_Reset(t *testing.T) {
	rb := NewRingBuffer(10)
	rb.Write([]byte("data"))
	rb.Reset()
	if rb.String() != "" {
		t.Errorf("after Reset: got %q, want empty", rb.String())
	}
}

// TestRingBuffer_Bytes_ReturnsCopy 验证 Bytes 返回副本, 修改不影响内部状态。
func TestRingBuffer_Bytes_ReturnsCopy(t *testing.T) {
	rb := NewRingBuffer(10)
	rb.Write([]byte("abcd"))
	out := rb.Bytes()
	out[0] = 'X' // 修改副本
	if !bytes.Equal(rb.Bytes(), []byte("abcd")) {
		t.Error("modifying Bytes() output affected internal state")
	}
}
