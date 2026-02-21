package runner

import "sync"

// RingBuffer 环形缓冲区，保留最近 N 行终端输出。
type RingBuffer struct {
	mu    sync.Mutex
	data  []byte
	limit int
}

// NewRingBuffer 创建容量为 maxLines 的环形缓冲区。
// limit 按字节数控制 (2000 行 × ~80 字符 ≈ 160KB)。
func NewRingBuffer(maxLines int) *RingBuffer {
	return &RingBuffer{
		data:  make([]byte, 0, maxLines*80),
		limit: maxLines * 80,
	}
}

// Write 追加数据，超出容量则丢弃旧数据 (复用底层数组, 避免 GC 分配)。
func (rb *RingBuffer) Write(p []byte) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	rb.data = append(rb.data, p...)
	if len(rb.data) > rb.limit {
		excess := len(rb.data) - rb.limit
		// copy 左移, 截断 — 复用底层数组, 无新分配
		n := copy(rb.data, rb.data[excess:])
		rb.data = rb.data[:n]
	}
}

// Bytes 返回缓冲区内容的副本。
func (rb *RingBuffer) Bytes() []byte {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	out := make([]byte, len(rb.data))
	copy(out, rb.data)
	return out
}

// String 返回缓冲区内容。
func (rb *RingBuffer) String() string {
	return string(rb.Bytes())
}

// Reset 清空缓冲区。
func (rb *RingBuffer) Reset() {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	rb.data = rb.data[:0]
}
