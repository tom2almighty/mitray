package platform

import (
	"testing"
	"unicode/utf8"
)

func TestOutputBufferTruncatesAtUTF8Boundary(t *testing.T) {
	buffer := newOutputBuffer(5)
	chunks := [][]byte{
		[]byte("x你"),
		[]byte("好"),
		[]byte("end"),
	}
	total := 0
	for _, chunk := range chunks {
		total += len(chunk)
		n, err := buffer.Write(chunk)
		if err != nil || n != len(chunk) {
			t.Fatalf("Write(%q) = %d, %v", chunk, n, err)
		}
		if len(buffer.data) > buffer.max {
			t.Fatalf("buffer length %d exceeds max %d", len(buffer.data), buffer.max)
		}
	}
	if total <= buffer.max {
		t.Fatal("test input did not exercise truncation")
	}
	got := buffer.String()
	if !utf8.ValidString(got) {
		t.Fatalf("truncated invalid UTF-8: %q", got)
	}
	if len(got) > buffer.max {
		t.Fatalf("got %d bytes, max %d", len(got), buffer.max)
	}
}
