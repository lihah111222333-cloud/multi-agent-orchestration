package util

import "io"

// LimitedWriter 限制写入字节数, 超出后静默丢弃 (防止内存耗尽)。
//
// 语义: 超限时返回 len(p) 而非 (0, ErrShortWrite), 避免 exec.Cmd 等
// 调用方误认为管道断裂。未超限时返回实际写入字节数以满足 io.Writer 契约。
type LimitedWriter struct {
	w       io.Writer
	limit   int
	written int
}

// NewLimitedWriter 创建 LimitedWriter。
func NewLimitedWriter(w io.Writer, limit int) *LimitedWriter {
	return &LimitedWriter{w: w, limit: limit}
}

// Write 写入 p, 超限后静默丢弃。
func (lw *LimitedWriter) Write(p []byte) (int, error) {
	remain := lw.limit - lw.written
	if remain <= 0 {
		return len(p), nil // 静默丢弃, 对调用方透明
	}
	if len(p) > remain {
		p = p[:remain]
	}
	n, err := lw.w.Write(p)
	lw.written += n
	return n, err
}

// Overflow 返回写入是否已超出限制 (后续写入被静默丢弃)。
func (lw *LimitedWriter) Overflow() bool { return lw.written >= lw.limit }

// Written 返回实际已写入的字节数。
func (lw *LimitedWriter) Written() int { return lw.written }
