package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"unicode/utf8"

	"pocketcli/internal/ssh"
	"pocketcli/internal/tailscale"
	"pocketcli/internal/tui/event"
	"pocketcli/internal/tui/renderer"
	"pocketcli/internal/tui/terminal"
)

const (
	hostsCompactWidth = 64
	hostNameMaxRunes  = 80
	hostsTitle        = "Pocket hosts"
	hostsHelpLine     = "j/k para navegar • l/Enter para conectar • q para sair"
	hostsTTYPath      = "/dev/tty"
	hostsListStartRow = 3
	hostsSelectedFill = ' '
)

var (
	errHostsTUIInterrupted = errors.New("hosts tui interrupted")
	errHostsNoItems        = errors.New("pocket: nenhum item para exibir")
)

type statusFetcher func() (tailscale.Status, error)
type hostOpener func(string) error
type hostPickerFunc func(in io.Reader, out io.Writer, hosts []Host) (hostPickResult, error)

var pickHosts hostPickerFunc = defaultHostPicker

type Host struct {
	Name   string
	IP     string
	OS     string
	Online bool
}

type tuiLastAction string

const (
	tuiActionUp     tuiLastAction = "up"
	tuiActionDown   tuiLastAction = "down"
	tuiActionSelect tuiLastAction = "select"
	tuiActionQuit   tuiLastAction = "quit"
	tuiActionNoop   tuiLastAction = "noop"
)

type State struct {
	Hosts      []Host
	Selected   int
	Total      int
	Running    bool
	LastAction tuiLastAction
}

type hostPickResult struct {
	Index    int
	Selected bool
}

type hostPickOutcome uint8

const (
	hostPickOutcomeNoop hostPickOutcome = iota
	hostPickOutcomeMoved
	hostPickOutcomeSelected
	hostPickOutcomeQuit
	hostPickOutcomeInterrupted
)

type tuiTerminal interface {
	RawMode() error
	Restore() error
	EnterAlternateScreen() error
	ExitAlternateScreen() error
	Size() (cols, rows uint16, err error)
	ResizeChan() <-chan struct{}
}

type tuiRenderer interface {
	WriteString(row, col uint16, s string, fg, bg uint8)
	Clear()
	ClearRegion(rowStart, rowEnd uint16)
	Flush() error
	Resize(cols, rows uint16) error
}

type hostPicker struct {
	state   State
	term    tuiTerminal
	loop    *event.EventLoop
	ren     tuiRenderer
	signals <-chan os.Signal
	cols    uint16
	rows    uint16
}

type hostPickStep struct {
	kind     hostPickOutcome
	previous int
}

func newHostsState(hosts []Host) State {
	return State{
		Hosts:      append([]Host(nil), hosts...),
		Selected:   0,
		Total:      len(hosts),
		Running:    true,
		LastAction: tuiActionNoop,
	}
}

func newHostPicker(term tuiTerminal, loop *event.EventLoop, ren tuiRenderer, hosts []Host, signals <-chan os.Signal) *hostPicker {
	return &hostPicker{
		state:   newHostsState(hosts),
		term:    term,
		loop:    loop,
		ren:     ren,
		signals: signals,
	}
}

func defaultHostPicker(_ io.Reader, _ io.Writer, hosts []Host) (hostPickResult, error) {
	tty, err := os.OpenFile(hostsTTYPath, os.O_RDWR, 0)
	if err != nil {
		return hostPickResult{}, fmt.Errorf("falha ao abrir terminal interativo: %w", err)
	}
	defer tty.Close()

	term, err := terminal.New(int(tty.Fd()))
	if err != nil {
		return hostPickResult{}, fmt.Errorf("falha ao preparar terminal interativo: %w", err)
	}

	cols, rows, err := term.Size()
	if err != nil {
		return hostPickResult{}, fmt.Errorf("falha ao obter tamanho do terminal: %w", err)
	}

	ren, err := renderer.New(tty, cols, rows)
	if err != nil {
		return hostPickResult{}, fmt.Errorf("falha ao preparar renderer da TUI: %w", err)
	}

	loop := event.New(tty, term.ResizeChan(), 0)
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signals)

	picker := newHostPicker(term, loop, ren, hosts, signals)
	return picker.Run(context.Background())
}

func (p *hostPicker) Run(ctx context.Context) (result hostPickResult, err error) {
	if p == nil || p.term == nil || p.loop == nil || p.ren == nil {
		return hostPickResult{}, fmt.Errorf("hosts tui: dependências inválidas")
	}
	if p.state.Total == 0 {
		return hostPickResult{}, errHostsNoItems
	}

	cols, rows, err := p.term.Size()
	if err != nil {
		return hostPickResult{}, fmt.Errorf("falha ao obter tamanho do terminal: %w", err)
	}
	p.cols = cols
	p.rows = rows

	if err := p.term.RawMode(); err != nil {
		return hostPickResult{}, fmt.Errorf("falha ao ativar raw mode: %w", err)
	}
	rawEnabled := true

	if err := p.term.EnterAlternateScreen(); err != nil {
		if rawEnabled {
			_ = p.term.Restore()
		}
		return hostPickResult{}, fmt.Errorf("falha ao abrir alternate screen: %w", err)
	}
	altEnabled := true

	loopCtx, cancelLoop := context.WithCancel(ctx)
	defer cancelLoop()

	defer func() {
		p.teardown(altEnabled, rawEnabled)
	}()

	if err := p.renderAll(); err != nil {
		return hostPickResult{}, err
	}
	if err := p.ren.Flush(); err != nil {
		return hostPickResult{}, fmt.Errorf("falha ao renderizar TUI: %w", err)
	}

	p.loop.Start(loopCtx)

	for {
		select {
		case <-ctx.Done():
			return hostPickResult{}, ctx.Err()
		case sig := <-p.signals:
			if sig == syscall.SIGINT || sig == syscall.SIGTERM {
				p.state.Running = false
				p.state.LastAction = tuiActionQuit
				return hostPickResult{}, errHostsTUIInterrupted
			}
		case evt, ok := <-p.loop.Events():
			if !ok {
				return hostPickResult{}, nil
			}

			switch evt.Type {
			case event.EventResize:
				if err := p.handleResize(); err != nil {
					return hostPickResult{}, err
				}
			case event.EventKey:
				step := p.applyKey(evt.Key)
				switch step.kind {
				case hostPickOutcomeMoved:
					if err := p.renderSelectionChange(step.previous); err != nil {
						return hostPickResult{}, err
					}
					if err := p.ren.Flush(); err != nil {
						return hostPickResult{}, fmt.Errorf("falha ao atualizar TUI: %w", err)
					}
				case hostPickOutcomeSelected:
					return hostPickResult{Index: p.state.Selected, Selected: true}, nil
				case hostPickOutcomeQuit:
					return hostPickResult{}, nil
				case hostPickOutcomeInterrupted:
					return hostPickResult{}, errHostsTUIInterrupted
				}
			}
		}
	}
}

func (p *hostPicker) applyKey(key event.KeyEvent) hostPickStep {
	step := hostPickStep{kind: hostPickOutcomeNoop, previous: p.state.Selected}
	switch key.Key {
	case event.KeyEnter:
		p.state.Running = false
		p.state.LastAction = tuiActionSelect
		step.kind = hostPickOutcomeSelected
	case event.KeyCtrlC:
		p.state.Running = false
		p.state.LastAction = tuiActionQuit
		step.kind = hostPickOutcomeInterrupted
	case event.KeyRune:
		switch key.Rune {
		case 'j':
			if p.state.Selected < p.state.Total-1 {
				p.state.Selected++
				p.state.LastAction = tuiActionDown
				step.kind = hostPickOutcomeMoved
				return step
			}
		case 'k':
			if p.state.Selected > 0 {
				p.state.Selected--
				p.state.LastAction = tuiActionUp
				step.kind = hostPickOutcomeMoved
				return step
			}
		case 'l':
			p.state.Running = false
			p.state.LastAction = tuiActionSelect
			step.kind = hostPickOutcomeSelected
			return step
		case 'q':
			p.state.Running = false
			p.state.LastAction = tuiActionQuit
			step.kind = hostPickOutcomeQuit
			return step
		}
	}

	p.state.LastAction = tuiActionNoop
	return step
}

func (p *hostPicker) handleResize() error {
	cols, rows, err := p.term.Size()
	if err != nil {
		return fmt.Errorf("falha ao redimensionar terminal: %w", err)
	}

	p.cols = cols
	p.rows = rows

	if err := p.ren.Resize(cols, rows); err != nil {
		return fmt.Errorf("falha ao redimensionar renderer: %w", err)
	}
	if err := p.renderAll(); err != nil {
		return err
	}
	if err := p.ren.Flush(); err != nil {
		return fmt.Errorf("falha ao renderizar TUI redimensionada: %w", err)
	}
	return nil
}

func (p *hostPicker) renderAll() error {
	p.ren.Clear()
	p.writeRow(1, hostsTitle, renderer.ColorBrightWhite, renderer.ColorReset)

	for i := range p.state.Hosts {
		p.renderHost(i)
	}

	p.writeRow(p.helpRow(), hostsHelpLine, renderer.ColorBrightBlack, renderer.ColorReset)
	return nil
}

func (p *hostPicker) renderSelectionChange(previous int) error {
	p.renderHost(previous)
	p.renderHost(p.state.Selected)
	return nil
}

func (p *hostPicker) renderHost(index int) {
	if index < 0 || index >= p.state.Total {
		return
	}

	startRow := p.hostRow(index)
	endRow := startRow + uint16(p.rowsPerHost()-1)
	p.ren.ClearRegion(startRow, endRow)

	host := p.state.Hosts[index]
	selected := index == p.state.Selected

	if p.compact() {
		name := fitText(host.Name, minInt(int(p.cols)-4, hostNameMaxRunes))
		details := fitText(fmt.Sprintf("%s • %s • %s", host.IP, host.OS, hostStatus(host)), minInt(int(p.cols)-4, hostNameMaxRunes))
		p.writeHostRow(startRow, fmt.Sprintf("%s%s", hostCursor(selected), name), selected)
		p.writeHostRow(startRow+1, "  "+details, selected)
		return
	}

	nameLimit := minInt(int(p.cols)-6, hostNameMaxRunes)
	name := fitText(host.Name, nameLimit)
	detailLimit := minInt(int(p.cols)-8-len([]rune(name)), hostNameMaxRunes)
	details := fitText(fmt.Sprintf("%s • %s • %s", host.IP, host.OS, hostStatus(host)), detailLimit)
	line := fmt.Sprintf("%s%s  (%s)", hostCursor(selected), name, details)
	p.writeHostRow(startRow, line, selected)
}

func (p *hostPicker) writeHostRow(row uint16, line string, selected bool) {
	if selected {
		p.writeRow(row, padToWidth(line, int(p.cols), hostsSelectedFill), renderer.ColorBlack, renderer.ColorBrightWhite)
		return
	}
	p.writeRow(row, line, renderer.ColorWhite, renderer.ColorReset)
}

func (p *hostPicker) writeRow(row uint16, line string, fg, bg uint8) {
	if row == 0 {
		return
	}
	p.ren.WriteString(row, 1, fitText(line, int(p.cols)), fg, bg)
}

func (p *hostPicker) compact() bool {
	return p.cols > 0 && p.cols < hostsCompactWidth
}

func (p *hostPicker) rowsPerHost() int {
	if p.compact() {
		return 2
	}
	return 1
}

func (p *hostPicker) hostRow(index int) uint16 {
	return hostsListStartRow + uint16(index*p.rowsPerHost())
}

func (p *hostPicker) helpRow() uint16 {
	return hostsListStartRow + uint16(p.state.Total*p.rowsPerHost()) + 1
}

func (p *hostPicker) teardown(altEnabled, rawEnabled bool) {
	if !altEnabled {
		p.ren.Clear()
		_ = p.ren.Flush()
	}

	if altEnabled {
		_ = p.term.ExitAlternateScreen()
	}
	if rawEnabled {
		_ = p.term.Restore()
	}
}

func hostCursor(selected bool) string {
	if selected {
		return "> "
	}
	return "  "
}

func hostStatus(host Host) string {
	if host.Online {
		return "online"
	}
	return "offline"
}

func padToWidth(text string, width int, fill rune) string {
	if width <= 0 {
		return ""
	}

	runes := []rune(text)
	if len(runes) >= width {
		return string(runes[:width])
	}

	var b strings.Builder
	b.Grow(width)
	b.WriteString(text)
	for i := len(runes); i < width; i++ {
		b.WriteRune(fill)
	}
	return b.String()
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func fitText(text string, max int) string {
	if max <= 1 {
		return ""
	}
	if utf8.RuneCountInString(text) <= max {
		return text
	}
	runes := []rune(text)
	return string(runes[:max-1]) + "…"
}

func toHosts(machines []tailscale.Machine) []Host {
	hosts := make([]Host, 0, len(machines))
	for _, machine := range machines {
		hosts = append(hosts, Host{
			Name:   machine.HostName,
			IP:     machine.IP,
			OS:     machine.OS,
			Online: machine.Online,
		})
	}
	return hosts
}

func runHostsTUI(in io.Reader, out io.Writer, fetch statusFetcher, openHost hostOpener) error {
	status, err := fetch()
	if err != nil {
		return err
	}

	machines := tailscale.MachinesFromStatus(status)
	if len(machines) == 0 {
		fmt.Fprintln(out, "Nenhum host encontrado.")
		return nil
	}

	result, err := pickHosts(in, out, toHosts(machines))
	if err != nil {
		return err
	}
	if !result.Selected {
		return nil
	}
	if result.Index < 0 || result.Index >= len(machines) {
		return fmt.Errorf("índice selecionado inválido: %d", result.Index)
	}

	selected := machines[result.Index]
	fmt.Fprintf(out, "Conectando em %s...\n", selected.HostName)
	return openHost(selected.HostName)
}

func defaultHostsViewer(in io.Reader, out io.Writer) error {
	return runHostsTUI(in, out, tailscale.GetStatus, ssh.Open)
}
