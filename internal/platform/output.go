package platform

import "sync"

type outputBuffer struct {
	mu   sync.Mutex
	max  int
	data []byte
}

func newOutputBuffer(max int) *outputBuffer {
	return &outputBuffer{max: max}
}

func (b *outputBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data = append(b.data, p...)
	if len(b.data) > b.max {
		cut := len(b.data) - b.max
		for cut < len(b.data) && b.data[cut]&0xc0 == 0x80 {
			cut++
		}
		b.data = append(b.data[:0], b.data[cut:]...)
	}
	return len(p), nil
}

func (b *outputBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.data)
}
