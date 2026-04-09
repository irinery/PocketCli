package event

import (
	"bytes"
	"context"
	"io"
	"sync/atomic"
	"testing"
	"time"
)

func TestT0201RuneInputEmitsKeyRune(t *testing.T) {
	loop, writer, cancel := newStartedLoop(t, nil, 0)
	defer cancel()

	writeAndClose(t, writer, []byte{'a'})

	events := collectUntilClosed(t, loop.Events(), 200*time.Millisecond)
	if len(events) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(events))
	}
	assertKeyEvent(t, events[0], KeyRune, 'a')
}

func TestT0202EscapeSequenceEmitsArrowUp(t *testing.T) {
	loop, writer, cancel := newStartedLoop(t, nil, 0)
	defer cancel()

	go func() {
		_, _ = writer.Write([]byte{0x1b})
		time.Sleep(10 * time.Millisecond)
		_, _ = writer.Write([]byte{'['})
		time.Sleep(10 * time.Millisecond)
		_, _ = writer.Write([]byte{'A'})
		_ = writer.Close()
	}()

	events := collectUntilClosed(t, loop.Events(), 250*time.Millisecond)
	if len(events) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(events))
	}
	assertKeyEvent(t, events[0], KeyArrowUp, 0)
}

func TestT0203IsolatedEscEmitsKeyEsc(t *testing.T) {
	loop, writer, cancel := newStartedLoop(t, nil, 0)
	defer cancel()

	go func() {
		_, _ = writer.Write([]byte{0x1b})
		time.Sleep(60 * time.Millisecond)
		_ = writer.Close()
	}()

	events := collectUntilClosed(t, loop.Events(), 250*time.Millisecond)
	if len(events) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(events))
	}
	assertKeyEvent(t, events[0], KeyEsc, 0)
}

func TestT0204CtrlCEmitsKeyCtrlC(t *testing.T) {
	loop, writer, cancel := newStartedLoop(t, nil, 0)
	defer cancel()

	writeAndClose(t, writer, []byte{0x03})

	events := collectUntilClosed(t, loop.Events(), 200*time.Millisecond)
	if len(events) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(events))
	}
	assertKeyEvent(t, events[0], KeyCtrlC, 0)
}

func TestT0205ResizeNotificationEmitsResizeEvent(t *testing.T) {
	resize := make(chan struct{}, 1)
	loop, writer, cancel := newStartedLoop(t, resize, 0)
	defer cancel()
	defer writer.Close()

	resize <- struct{}{}

	event := waitForEvent(t, loop.Events(), 100*time.Millisecond)
	if event.Type != EventResize {
		t.Fatalf("event.Type = %v, want %v", event.Type, EventResize)
	}
}

func TestT0206TickIntervalEmitsExactlyTwoTicks(t *testing.T) {
	loop, writer, cancel := newStartedLoop(t, nil, 200*time.Millisecond)
	defer cancel()
	defer writer.Close()

	start := time.Now()
	first := waitForEvent(t, loop.Events(), 260*time.Millisecond)
	firstElapsed := time.Since(start)
	if first.Type != EventTick {
		t.Fatalf("first event type = %v, want %v", first.Type, EventTick)
	}
	if firstElapsed < 180*time.Millisecond || firstElapsed > 280*time.Millisecond {
		t.Fatalf("first tick arrived after %v, want between 180ms and 280ms", firstElapsed)
	}

	second := waitForEvent(t, loop.Events(), 260*time.Millisecond)
	secondElapsed := time.Since(start)
	if second.Type != EventTick {
		t.Fatalf("second event type = %v, want %v", second.Type, EventTick)
	}
	if secondElapsed < 380*time.Millisecond || secondElapsed > 480*time.Millisecond {
		t.Fatalf("second tick arrived after %v, want between 380ms and 480ms", secondElapsed)
	}

	remaining := 450*time.Millisecond - time.Since(start)
	if remaining < 0 {
		remaining = 0
	}

	select {
	case event := <-loop.Events():
		t.Fatalf("unexpected extra event before 450ms: %+v", event)
	case <-time.After(remaining):
	}
}

func TestT0207FullEventBufferDropsNewestEvent(t *testing.T) {
	loop, writer, cancel := newStartedLoop(t, nil, 0)
	defer cancel()

	writeAndClose(t, writer, bytes.Repeat([]byte{'a'}, eventBufferSize+1))

	events := collectUntilClosed(t, loop.Events(), 500*time.Millisecond)
	if len(events) != eventBufferSize {
		t.Fatalf("len(events) = %d, want %d", len(events), eventBufferSize)
	}
	if dropped := atomic.LoadUint64(&loop.DroppedEvents); dropped != 1 {
		t.Fatalf("DroppedEvents = %d, want 1", dropped)
	}
}

func TestT0208CancelClosesEventChannel(t *testing.T) {
	loop, writer, cancel := newStartedLoop(t, nil, 0)
	defer writer.Close()

	cancel()

	select {
	case _, ok := <-loop.Events():
		if ok {
			t.Fatal("Events() remained open after cancel")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Events() did not close within 100ms")
	}
}

func TestT0209BurstDoesNotLoseEventsSilently(t *testing.T) {
	loop, writer, cancel := newStartedLoop(t, nil, 0)
	defer cancel()

	const burstSize = 10000
	writeAndClose(t, writer, bytes.Repeat([]byte{'a'}, burstSize))

	events := collectUntilClosed(t, loop.Events(), 2*time.Second)
	emitted := len(events)
	dropped := int(atomic.LoadUint64(&loop.DroppedEvents))
	if emitted+dropped != burstSize {
		t.Fatalf("emitted + dropped = %d, want %d", emitted+dropped, burstSize)
	}
}

func TestT0210IncompleteEscapeSequenceEmitsEscAndDiscardsRemainder(t *testing.T) {
	loop, writer, cancel := newStartedLoop(t, nil, 0)
	defer cancel()

	go func() {
		_, _ = writer.Write([]byte{0x1b, '['})
		time.Sleep(60 * time.Millisecond)
		_ = writer.Close()
	}()

	events := collectUntilClosed(t, loop.Events(), 250*time.Millisecond)
	if len(events) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(events))
	}
	assertKeyEvent(t, events[0], KeyEsc, 0)
}

func newStartedLoop(t *testing.T, resize <-chan struct{}, tickInterval time.Duration) (*EventLoop, *io.PipeWriter, context.CancelFunc) {
	t.Helper()

	reader, writer := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())

	loop := New(reader, resize, tickInterval)
	loop.Start(ctx)

	t.Cleanup(func() {
		cancel()
		_ = writer.Close()
		_ = reader.Close()
	})

	return loop, writer, cancel
}

func writeAndClose(t *testing.T, writer *io.PipeWriter, data []byte) {
	t.Helper()

	if _, err := writer.Write(data); err != nil {
		t.Fatalf("writer.Write() error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("writer.Close() error = %v", err)
	}
}

func collectUntilClosed(t *testing.T, events <-chan Event, timeout time.Duration) []Event {
	t.Helper()

	deadline := time.After(timeout)
	var collected []Event

	for {
		select {
		case event, ok := <-events:
			if !ok {
				return collected
			}
			collected = append(collected, event)
		case <-deadline:
			t.Fatalf("timed out waiting for event channel to close after %v", timeout)
		}
	}
}

func waitForEvent(t *testing.T, events <-chan Event, timeout time.Duration) Event {
	t.Helper()

	select {
	case event := <-events:
		return event
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for event after %v", timeout)
		return Event{}
	}
}

func assertKeyEvent(t *testing.T, event Event, key Key, r rune) {
	t.Helper()

	if event.Type != EventKey {
		t.Fatalf("event.Type = %v, want %v", event.Type, EventKey)
	}
	if event.Key.Key != key {
		t.Fatalf("event.Key.Key = %v, want %v", event.Key.Key, key)
	}
	if key == KeyRune && event.Key.Rune != r {
		t.Fatalf("event.Key.Rune = %q, want %q", event.Key.Rune, r)
	}
}
