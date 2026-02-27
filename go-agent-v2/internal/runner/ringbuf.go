package runner

import "sync"

type RingBuffer struct {
	mu    sync.Mutex
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
		n := copy(rb.data, rb.data[excess:])
		rb.data = rb.data[:n]
	}
}

func (rb *RingBuffer) Bytes() []byte {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	out := make([]byte, len(rb.data))
	copy(out, rb.data)
	return out
}

func (rb *RingBuffer) String() string {
	return string(rb.Bytes())
}

func (rb *RingBuffer) Reset() {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	rb.data = rb.data[:0]
}
