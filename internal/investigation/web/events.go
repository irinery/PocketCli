package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"
)

type event struct {
	EventID    uint64 `json:"event_id"`
	Type       string `json:"type"`
	RunID      string `json:"run_id,omitempty"`
	SnapshotID string `json:"snapshot_id,omitempty"`
	Timestamp  string `json:"timestamp"`
	State      string `json:"state,omitempty"`
	Message    string `json:"message"`
}
type eventHub struct {
	mu          sync.Mutex
	next        uint64
	history     []event
	subscribers map[chan event]struct{}
}

func newEventHub() *eventHub { return &eventHub{subscribers: map[chan event]struct{}{}} }
func (h *eventHub) publish(kind, runID, snapshotID, state, message string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.next++
	e := event{EventID: h.next, Type: kind, RunID: runID, SnapshotID: snapshotID, Timestamp: time.Now().UTC().Format(time.RFC3339Nano), State: state, Message: message}
	h.history = append(h.history, e)
	if len(h.history) > 1000 {
		h.history = h.history[len(h.history)-1000:]
	}
	for ch := range h.subscribers {
		select {
		case ch <- e:
		default:
		}
	}
}
func (h *eventHub) subscribe(last uint64) ([]event, chan event, func(), bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	expired := len(h.history) > 0 && last > 0 && last < h.history[0].EventID-1
	past := []event{}
	if !expired {
		for _, e := range h.history {
			if e.EventID > last {
				past = append(past, e)
			}
		}
	}
	ch := make(chan event, 32)
	h.subscribers[ch] = struct{}{}
	return past, ch, func() { h.mu.Lock(); delete(h.subscribers, ch); close(ch); h.mu.Unlock() }, expired
}
func (h *Handler) serveEvents(w http.ResponseWriter, r *http.Request, sid string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		h.writeError(w, 500, "ERR_EVENTS_UNAVAILABLE", "SSE não é suportada neste servidor.")
		return
	}
	last := uint64(0)
	if value := r.Header.Get("Last-Event-ID"); value != "" {
		last, _ = strconv.ParseUint(value, 10, 64)
	}
	past, ch, closeSub, expired := h.events.subscribe(last)
	defer closeSub()
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	if expired {
		_, _ = fmt.Fprint(w, "event: resync_required\ndata: {\"type\":\"resync_required\"}\n\n")
	} else {
		for _, e := range past {
			writeSSE(w, e)
		}
	}
	flusher.Flush()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case e, ok := <-ch:
			if !ok {
				return
			}
			writeSSE(w, e)
			flusher.Flush()
		case <-ticker.C:
			if !h.sessionActive(sid) {
				return
			}
			_, _ = fmt.Fprint(w, ": heartbeat\n\n")
			flusher.Flush()
		}
	}
}
func writeSSE(w http.ResponseWriter, e event) {
	data, _ := json.Marshal(e)
	_, _ = fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", e.EventID, e.Type, data)
}
func (h *Handler) sessionActive(sid string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	s, ok := h.sessions[sid]
	return ok && time.Now().Before(s.expires)
}
