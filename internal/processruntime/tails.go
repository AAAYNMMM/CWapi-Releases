package processruntime

import (
	"strings"
	"sync"
	"time"

	"github.com/AAAYNMMM/CWapi/internal/observability"
)

type Tails struct {
	Stdout *tailWriter
	Stderr *tailWriter
	mu     sync.Mutex
	stream string
	at     int64
}

type tailWriter struct {
	mu     sync.Mutex
	data   []byte
	owner  *Tails
	stream string
}

func NewTails() *Tails {
	tails := &Tails{}
	tails.Stdout = &tailWriter{owner: tails, stream: "stdout"}
	tails.Stderr = &tailWriter{owner: tails, stream: "stderr"}
	return tails
}

func (t *Tails) Snapshot() (string, string) {
	if t == nil {
		return "", ""
	}
	return t.Stdout.public(), t.Stderr.public()
}

func (t *Tails) Latest() (string, int64) {
	if t == nil {
		return "", 0
	}
	t.mu.Lock()
	stream, at := t.stream, t.at
	t.mu.Unlock()
	return stream, at
}

func (w *tailWriter) Write(payload []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	length := len(payload)
	if length >= TailBytes {
		w.data = append(w.data[:0], payload[length-TailBytes:]...)
		w.markLatest(length)
		return length, nil
	}
	overflow := len(w.data) + length - TailBytes
	if overflow > 0 {
		copy(w.data, w.data[overflow:])
		w.data = w.data[:len(w.data)-overflow]
	}
	w.data = append(w.data, payload...)
	w.markLatest(length)
	return length, nil
}

func (w *tailWriter) markLatest(length int) {
	if length > 0 && w.owner != nil {
		w.owner.mu.Lock()
		w.owner.stream = w.stream
		w.owner.at = time.Now().UnixMilli()
		w.owner.mu.Unlock()
	}
}

func (w *tailWriter) public() string {
	w.mu.Lock()
	value := append([]byte(nil), w.data...)
	w.mu.Unlock()
	redacted := observability.Redact(strings.ToValidUTF8(string(value), "?"))
	if len(redacted) > TailBytes {
		redacted = redacted[len(redacted)-TailBytes:]
	}
	return strings.ToValidUTF8(redacted, "?")
}
