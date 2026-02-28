package runner

import "sync"

type RingBuffer struct {
	mu    sync.RWMutex
	data  []byte
	limit int
}

func NewRingBuffer(maxLines int) *RingBuffer {
	return &RingBuffer{data: make([]byte, 0, maxLines*80), limit: maxLines * 80}
}

func (rb *RingBuffer) Write(p []byte) {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	rb.data = append(rb.data, p...)
	if excess := len(rb.data) - rb.limit; excess > 0 {
		rb.data = rb.data[:copy(rb.data, rb.data[excess:])]
	}
}

func (rb *RingBuffer) Bytes() []byte {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	return append([]byte(nil), rb.data...)
}

func (rb *RingBuffer) String() string {
	return string(rb.Bytes())
}

func (rb *RingBuffer) Reset() {
	rb.mu.Lock()
	rb.data = rb.data[:0]
	rb.mu.Unlock()
}
