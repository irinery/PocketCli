package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"pocketcli/internal/tailscale"
	"pocketcli/internal/tui/event"
)

func TestT0101HostPickerStateMovesDownWithJ(t *testing.T) {
	picker := newHostPicker(nil, nil, nil, []Host{{Name: "a"}, {Name: "b"}, {Name: "c"}}, nil)

	step := picker.applyKey(event.KeyEvent{Key: event.KeyRune, Rune: 'j'})

	if step.kind != hostPickOutcomeMoved {
		t.Fatalf("step.kind = %v, want moved", step.kind)
	}
	if picker.state.Selected != 1 {
		t.Fatalf("Selected = %d, want 1", picker.state.Selected)
	}
	if picker.state.LastAction != tuiActionDown {
		t.Fatalf("LastAction = %q, want %q", picker.state.LastAction, tuiActionDown)
	}
}

func TestT0102HostPickerStateDoesNotMovePastEnd(t *testing.T) {
	picker := newHostPicker(nil, nil, nil, []Host{{Name: "a"}, {Name: "b"}, {Name: "c"}}, nil)
	picker.state.Selected = 2

	step := picker.applyKey(event.KeyEvent{Key: event.KeyRune, Rune: 'j'})

	if step.kind != hostPickOutcomeNoop {
		t.Fatalf("step.kind = %v, want noop", step.kind)
	}
	if picker.state.Selected != 2 {
		t.Fatalf("Selected = %d, want 2", picker.state.Selected)
	}
	if picker.state.LastAction != tuiActionNoop {
		t.Fatalf("LastAction = %q, want %q", picker.state.LastAction, tuiActionNoop)
	}
}

func TestT0103HostPickerStateDoesNotMoveBeforeStart(t *testing.T) {
	picker := newHostPicker(nil, nil, nil, []Host{{Name: "a"}, {Name: "b"}, {Name: "c"}}, nil)

	step := picker.applyKey(event.KeyEvent{Key: event.KeyRune, Rune: 'k'})

	if step.kind != hostPickOutcomeNoop {
		t.Fatalf("step.kind = %v, want noop", step.kind)
	}
	if picker.state.Selected != 0 {
		t.Fatalf("Selected = %d, want 0", picker.state.Selected)
	}
	if picker.state.LastAction != tuiActionNoop {
		t.Fatalf("LastAction = %q, want %q", picker.state.LastAction, tuiActionNoop)
	}
}

func TestT0104HostPickerStateMovesUpWithK(t *testing.T) {
	picker := newHostPicker(nil, nil, nil, []Host{{Name: "a"}, {Name: "b"}, {Name: "c"}}, nil)
	picker.state.Selected = 1

	step := picker.applyKey(event.KeyEvent{Key: event.KeyRune, Rune: 'k'})

	if step.kind != hostPickOutcomeMoved {
		t.Fatalf("step.kind = %v, want moved", step.kind)
	}
	if picker.state.Selected != 0 {
		t.Fatalf("Selected = %d, want 0", picker.state.Selected)
	}
	if picker.state.LastAction != tuiActionUp {
		t.Fatalf("LastAction = %q, want %q", picker.state.LastAction, tuiActionUp)
	}
}

func TestT0105HostPickerRunSelectsWithL(t *testing.T) {
	harness := newHostPickerHarness([]Host{{Name: "a"}, {Name: "b"}, {Name: "c"}}, 80, 24)
	defer harness.cleanup()

	done := harness.run()
	harness.write(t, []byte{'j', 'l'})

	result := waitHostPickerResult(t, done, 250*time.Millisecond)
	if result.err != nil {
		t.Fatalf("Run() error = %v", result.err)
	}
	if !result.pick.Selected {
		t.Fatal("Selected = false, want true")
	}
	if result.pick.Index != 1 {
		t.Fatalf("Index = %d, want 1", result.pick.Index)
	}
}

func TestT0106HostPickerRunQuitsWithQ(t *testing.T) {
	harness := newHostPickerHarness([]Host{{Name: "a"}, {Name: "b"}, {Name: "c"}}, 80, 24)
	defer harness.cleanup()

	done := harness.run()
	harness.write(t, []byte{'q'})

	result := waitHostPickerResult(t, done, 250*time.Millisecond)
	if result.err != nil {
		t.Fatalf("Run() error = %v, want nil", result.err)
	}
	if result.pick.Selected {
		t.Fatalf("Selected = true, want false: %#v", result.pick)
	}
	if got := harness.ren.flushCount(); got != 1 {
		t.Fatalf("flush count = %d, want 1", got)
	}
	assertCallSubsequence(t, harness.term.calls(), []string{"term.raw", "term.enter_alt", "term.exit_alt", "term.restore"})
}

func TestT0107HostPickerRunCtrlCReturnsInterruptedAndRestoresTerminal(t *testing.T) {
	harness := newHostPickerHarness([]Host{{Name: "a"}, {Name: "b"}, {Name: "c"}}, 80, 24)
	defer harness.cleanup()

	done := harness.run()
	harness.write(t, []byte{0x03})

	result := waitHostPickerResult(t, done, 250*time.Millisecond)
	if !errors.Is(result.err, errHostsTUIInterrupted) {
		t.Fatalf("Run() error = %v, want %v", result.err, errHostsTUIInterrupted)
	}
	assertCallSubsequence(t, harness.term.calls(), []string{"term.raw", "term.enter_alt", "term.exit_alt", "term.restore"})
}

func TestT0108HostPickerRunIgnoresUnknownAndEscapeKeysWithoutRedraw(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
	}{
		{name: "unknown-rune", input: []byte{'x'}},
		{name: "arrow-sequence", input: []byte{0x1b, '[', 'B'}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := newHostPickerHarness([]Host{{Name: "a"}, {Name: "b"}, {Name: "c"}}, 80, 24)
			defer harness.cleanup()

			done := harness.run()
			harness.writeAndClose(t, tt.input)

			result := waitHostPickerResult(t, done, 250*time.Millisecond)
			if result.err != nil {
				t.Fatalf("Run() error = %v", result.err)
			}
			if harness.picker.state.Selected != 0 {
				t.Fatalf("Selected = %d, want 0", harness.picker.state.Selected)
			}
			if got := harness.ren.flushCount(); got != 1 {
				t.Fatalf("flush count = %d, want 1", got)
			}
		})
	}
}

func TestT0109HostPickerRunDiscardsLongANSISequence(t *testing.T) {
	harness := newHostPickerHarness([]Host{{Name: "a"}, {Name: "b"}, {Name: "c"}}, 80, 24)
	defer harness.cleanup()

	done := harness.run()
	harness.writeAndClose(t, []byte{0x1b, '[', '2', '0', '0', '~'})

	result := waitHostPickerResult(t, done, 250*time.Millisecond)
	if result.err != nil {
		t.Fatalf("Run() error = %v", result.err)
	}
	if harness.picker.state.Selected != 0 {
		t.Fatalf("Selected = %d, want 0", harness.picker.state.Selected)
	}
	if got := harness.ren.flushCount(); got != 1 {
		t.Fatalf("flush count = %d, want 1", got)
	}
}

func TestT0110HostPickerStateSingleItemStaysSelected(t *testing.T) {
	picker := newHostPicker(nil, nil, nil, []Host{{Name: "only"}}, nil)

	picker.applyKey(event.KeyEvent{Key: event.KeyRune, Rune: 'j'})
	picker.applyKey(event.KeyEvent{Key: event.KeyRune, Rune: 'k'})

	if picker.state.Selected != 0 {
		t.Fatalf("Selected = %d, want 0", picker.state.Selected)
	}
}

func TestRunHostsTUIConnectsSelectedHost(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	orig := pickHosts
	t.Cleanup(func() { pickHosts = orig })

	pickHosts = func(in io.Reader, out io.Writer, hosts []Host) (hostPickResult, error) {
		return hostPickResult{Index: 1, Selected: true}, nil
	}

	fakeStatus := tailscale.Status{Peer: map[string]tailscale.Peer{
		"a": {HostName: "prod-api", Online: true, OS: "linux", TailscaleIPs: []string{"100.64.0.1"}},
		"b": {HostName: "dev-db", Online: false, OS: "linux", TailscaleIPs: []string{"100.64.0.2"}},
	}}

	var opened string
	err := runHostsTUI(strings.NewReader(""), &bytes.Buffer{}, func() (tailscale.Status, error) {
		return fakeStatus, nil
	}, func(host string) error {
		opened = host
		return nil
	})
	if err != nil {
		t.Fatalf("runHostsTUI returned error: %v", err)
	}
	if opened != "prod-api" {
		t.Fatalf("opened = %q, want %q", opened, "prod-api")
	}
}

func TestRunHostsTUIHonorsQuitWithoutOpeningHost(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	orig := pickHosts
	t.Cleanup(func() { pickHosts = orig })

	pickHosts = func(in io.Reader, out io.Writer, hosts []Host) (hostPickResult, error) {
		return hostPickResult{}, nil
	}

	fakeStatus := tailscale.Status{Peer: map[string]tailscale.Peer{
		"a": {HostName: "prod-api", Online: true, OS: "linux", TailscaleIPs: []string{"100.64.0.1"}},
	}}

	called := false
	err := runHostsTUI(strings.NewReader(""), &bytes.Buffer{}, func() (tailscale.Status, error) {
		return fakeStatus, nil
	}, func(host string) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("runHostsTUI returned error: %v", err)
	}
	if called {
		t.Fatal("openHost called, want not called")
	}
}

func TestRunHostsTUIPropagatesInterruptedPicker(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	orig := pickHosts
	t.Cleanup(func() { pickHosts = orig })

	pickHosts = func(in io.Reader, out io.Writer, hosts []Host) (hostPickResult, error) {
		return hostPickResult{}, errHostsTUIInterrupted
	}

	fakeStatus := tailscale.Status{Peer: map[string]tailscale.Peer{
		"a": {HostName: "prod-api", Online: true, OS: "linux", TailscaleIPs: []string{"100.64.0.1"}},
	}}

	err := runHostsTUI(strings.NewReader(""), &bytes.Buffer{}, func() (tailscale.Status, error) {
		return fakeStatus, nil
	}, func(host string) error { return nil })
	if !errors.Is(err, errHostsTUIInterrupted) {
		t.Fatalf("runHostsTUI error = %v, want %v", err, errHostsTUIInterrupted)
	}
}

func TestRunHostsTUI_NoHosts(t *testing.T) {
	out := &bytes.Buffer{}
	err := runHostsTUI(strings.NewReader(""), out, func() (tailscale.Status, error) {
		return tailscale.Status{Peer: map[string]tailscale.Peer{}}, nil
	}, func(host string) error { return nil })
	if err != nil {
		t.Fatalf("runHostsTUI returned error: %v", err)
	}
	if !strings.Contains(out.String(), "Nenhum host encontrado.") {
		t.Fatalf("expected no host message, got: %q", out.String())
	}
}

func TestFitText_TruncatesWithEllipsis(t *testing.T) {
	got := fitText("abcdefghijkl", 6)
	if got != "abcde…" {
		t.Fatalf("fitText() = %q, want %q", got, "abcde…")
	}
}

type hostPickerHarness struct {
	picker *hostPicker
	term   *fakeHostTerminal
	ren    *fakeHostRenderer
	input  *io.PipeWriter
	reader *io.PipeReader
}

type hostPickerRunResult struct {
	pick hostPickResult
	err  error
}

func newHostPickerHarness(hosts []Host, cols, rows uint16) *hostPickerHarness {
	reader, writer := io.Pipe()
	term := newFakeHostTerminal(cols, rows)
	ren := newFakeHostRenderer(cols, rows)
	loop := event.New(reader, term.ResizeChan(), 0)

	return &hostPickerHarness{
		picker: newHostPicker(term, loop, ren, hosts, nil),
		term:   term,
		ren:    ren,
		input:  writer,
		reader: reader,
	}
}

func (h *hostPickerHarness) run() <-chan hostPickerRunResult {
	done := make(chan hostPickerRunResult, 1)
	go func() {
		pick, err := h.picker.Run(context.Background())
		done <- hostPickerRunResult{pick: pick, err: err}
	}()
	return done
}

func (h *hostPickerHarness) write(t *testing.T, data []byte) {
	t.Helper()

	if _, err := h.input.Write(data); err != nil {
		t.Fatalf("input.Write() error = %v", err)
	}
}

func (h *hostPickerHarness) writeAndClose(t *testing.T, data []byte) {
	t.Helper()
	h.write(t, data)
	h.cleanup()
}

func (h *hostPickerHarness) cleanup() {
	_ = h.input.Close()
	_ = h.reader.Close()
}

func waitHostPickerResult(t *testing.T, done <-chan hostPickerRunResult, timeout time.Duration) hostPickerRunResult {
	t.Helper()

	select {
	case result := <-done:
		return result
	case <-time.After(timeout):
		t.Fatalf("Run() did not finish within %v", timeout)
		return hostPickerRunResult{}
	}
}

type fakeHostTerminal struct {
	mu     sync.Mutex
	cols   uint16
	rows   uint16
	resize chan struct{}
	log    []string
}

func newFakeHostTerminal(cols, rows uint16) *fakeHostTerminal {
	return &fakeHostTerminal{
		cols:   cols,
		rows:   rows,
		resize: make(chan struct{}, 4),
	}
}

func (t *fakeHostTerminal) RawMode() error {
	t.record("term.raw")
	return nil
}

func (t *fakeHostTerminal) Restore() error {
	t.record("term.restore")
	return nil
}

func (t *fakeHostTerminal) EnterAlternateScreen() error {
	t.record("term.enter_alt")
	return nil
}

func (t *fakeHostTerminal) ExitAlternateScreen() error {
	t.record("term.exit_alt")
	return nil
}

func (t *fakeHostTerminal) Size() (cols, rows uint16, err error) {
	t.record("term.size")
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.cols, t.rows, nil
}

func (t *fakeHostTerminal) ResizeChan() <-chan struct{} {
	return t.resize
}

func (t *fakeHostTerminal) calls() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string(nil), t.log...)
}

func (t *fakeHostTerminal) record(call string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.log = append(t.log, call)
}

type fakeHostRenderer struct {
	mu      sync.Mutex
	cols    uint16
	rows    uint16
	flushes int
}

func newFakeHostRenderer(cols, rows uint16) *fakeHostRenderer {
	return &fakeHostRenderer{cols: cols, rows: rows}
}

func (r *fakeHostRenderer) WriteString(row, col uint16, s string, fg, bg uint8) {}

func (r *fakeHostRenderer) Clear() {}

func (r *fakeHostRenderer) ClearRegion(rowStart, rowEnd uint16) {}

func (r *fakeHostRenderer) Flush() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.flushes++
	return nil
}

func (r *fakeHostRenderer) Resize(cols, rows uint16) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cols = cols
	r.rows = rows
	return nil
}

func (r *fakeHostRenderer) flushCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.flushes
}

func assertCallSubsequence(t *testing.T, calls []string, expected []string) {
	t.Helper()

	index := 0
	for _, call := range calls {
		if index < len(expected) && call == expected[index] {
			index++
		}
	}
	if index != len(expected) {
		t.Fatalf("calls = %v, missing subsequence %v", calls, expected)
	}
}

func TestHostPickerRunSelectsWithEnter(t *testing.T) {
	harness := newHostPickerHarness([]Host{{Name: "a"}, {Name: "b"}}, 80, 24)
	defer harness.cleanup()

	done := harness.run()
	harness.write(t, []byte{'j', '\r'})

	result := waitHostPickerResult(t, done, 250*time.Millisecond)
	if result.err != nil {
		t.Fatalf("Run() error = %v", result.err)
	}
	if !result.pick.Selected || result.pick.Index != 1 {
		t.Fatalf("pick = %#v, want Selected=true Index=1", result.pick)
	}
}

func TestPadToWidthPadsWithoutTruncatingShortText(t *testing.T) {
	got := padToWidth("abc", 5, '.')
	if got != "abc.." {
		t.Fatalf("padToWidth() = %q, want %q", got, "abc..")
	}
}

func TestRunHostsTUIRejectsInvalidPickedIndex(t *testing.T) {
	orig := pickHosts
	t.Cleanup(func() { pickHosts = orig })

	pickHosts = func(in io.Reader, out io.Writer, hosts []Host) (hostPickResult, error) {
		return hostPickResult{Index: 99, Selected: true}, nil
	}

	fakeStatus := tailscale.Status{Peer: map[string]tailscale.Peer{
		"a": {HostName: "prod-api", Online: true, OS: "linux", TailscaleIPs: []string{"100.64.0.1"}},
	}}

	err := runHostsTUI(strings.NewReader(""), io.Discard, func() (tailscale.Status, error) {
		return fakeStatus, nil
	}, func(host string) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "índice selecionado inválido") {
		t.Fatalf("runHostsTUI error = %v, want invalid index", err)
	}
}

func TestHostPickerRunReturnsNoItemsError(t *testing.T) {
	picker := newHostPicker(newFakeHostTerminal(80, 24), event.New(bytes.NewReader(nil), nil, 0), newFakeHostRenderer(80, 24), nil, nil)

	_, err := picker.Run(context.Background())
	if !errors.Is(err, errHostsNoItems) {
		t.Fatalf("Run() error = %v, want %v", err, errHostsNoItems)
	}
}

func TestHostStatus(t *testing.T) {
	if got := hostStatus(Host{Online: true}); got != "online" {
		t.Fatalf("hostStatus(true) = %q, want %q", got, "online")
	}
	if got := hostStatus(Host{Online: false}); got != "offline" {
		t.Fatalf("hostStatus(false) = %q, want %q", got, "offline")
	}
}

func BenchmarkHostPickerApplyKey(b *testing.B) {
	picker := newHostPicker(nil, nil, nil, []Host{{Name: "a"}, {Name: "b"}, {Name: "c"}}, nil)
	keys := []event.KeyEvent{
		{Key: event.KeyRune, Rune: 'j'},
		{Key: event.KeyRune, Rune: 'k'},
		{Key: event.KeyRune, Rune: 'x'},
		{Key: event.KeyEnter},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := keys[i%len(keys)]
		step := picker.applyKey(key)
		if step.kind == hostPickOutcomeSelected {
			picker.state.Running = true
		}
	}
}

func ExamplepadToWidth() {
	fmt.Println(padToWidth("abc", 5, '.'))
	// Output: abc..
}
