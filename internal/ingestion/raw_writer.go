package ingestion

import (
	"fmt"
	"sync"
	"time"
)

type RawEvent struct {
	ID           string
	Sequence     int64
	Source       string
	Payload      []byte
	SourceTS     time.Time
	IngestTS     time.Time
	NormalizedTS time.Time
}

type MemoryRawWriter struct {
	mu    sync.RWMutex
	byID  map[string]RawEvent
	order []string
}

func NewMemoryRawWriter() *MemoryRawWriter {
	return &MemoryRawWriter{
		byID: make(map[string]RawEvent),
	}
}

func (w *MemoryRawWriter) Write(event RawEvent) (bool, error) {
	if event.ID == "" {
		return false, fmt.Errorf("event id is required")
	}
	if event.SourceTS.IsZero() || event.IngestTS.IsZero() || event.NormalizedTS.IsZero() {
		return false, fmt.Errorf("source_ts/ingest_ts/normalized_ts are required")
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if _, exists := w.byID[event.ID]; exists {
		return false, nil
	}

	stored := cloneEvent(event)
	w.byID[event.ID] = stored
	w.order = append(w.order, event.ID)
	return true, nil
}

func (w *MemoryRawWriter) Get(id string) (RawEvent, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	event, ok := w.byID[id]
	if !ok {
		return RawEvent{}, false
	}
	return cloneEvent(event), true
}

func (w *MemoryRawWriter) List() []RawEvent {
	w.mu.RLock()
	defer w.mu.RUnlock()

	out := make([]RawEvent, 0, len(w.order))
	for _, id := range w.order {
		evt := w.byID[id]
		out = append(out, cloneEvent(evt))
	}
	return out
}

func cloneEvent(event RawEvent) RawEvent {
	copyPayload := make([]byte, len(event.Payload))
	copy(copyPayload, event.Payload)
	event.Payload = copyPayload
	return event
}
