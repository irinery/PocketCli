package bubbletea

import (
	"bufio"
	"io"
	"os"
)

type Msg interface{}

type Cmd func() Msg

type Model interface {
	Init() Cmd
	Update(Msg) (Model, Cmd)
	View() string
}

type quitMsg struct{}

var Quit Cmd = func() Msg { return quitMsg{} }

type KeyType int

const (
	KeyRunes KeyType = iota
	KeyUp
	KeyDown
	KeyEnter
	KeyEsc
	KeyCtrlC
)

type KeyMsg struct {
	Type  KeyType
	Runes []rune
}

func (k KeyMsg) String() string {
	switch k.Type {
	case KeyUp:
		return "up"
	case KeyDown:
		return "down"
	case KeyEnter:
		return "enter"
	case KeyEsc:
		return "esc"
	case KeyCtrlC:
		return "ctrl+c"
	case KeyRunes:
		if len(k.Runes) > 0 {
			return string(k.Runes)
		}
	}
	return ""
}

type ProgramOption func(*Program)

type Program struct {
	model Model
	in    io.Reader
	out   io.Writer
}

func WithInput(in io.Reader) ProgramOption {
	return func(p *Program) { p.in = in }
}

func WithOutput(out io.Writer) ProgramOption {
	return func(p *Program) { p.out = out }
}

func NewProgram(model Model, opts ...ProgramOption) *Program {
	p := &Program{model: model, in: os.Stdin, out: os.Stdout}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

func (p *Program) Run() (Model, error) {
	if initCmd := p.model.Init(); initCmd != nil {
		if _, shouldQuit := initCmd().(quitMsg); shouldQuit {
			return p.model, nil
		}
	}

	if p.out != nil {
		_, _ = io.WriteString(p.out, p.model.View())
	}

	reader := bufio.NewReader(p.in)
	for {
		msg, ok := readKeyMsg(reader)
		if !ok {
			return p.model, nil
		}

		updated, cmd := p.model.Update(msg)
		p.model = updated

		if p.out != nil {
			_, _ = io.WriteString(p.out, p.model.View())
		}

		if cmd != nil {
			if _, shouldQuit := cmd().(quitMsg); shouldQuit {
				return p.model, nil
			}
		}
	}
}

func readKeyMsg(reader *bufio.Reader) (KeyMsg, bool) {
	b, err := reader.ReadByte()
	if err != nil {
		return KeyMsg{}, false
	}

	switch b {
	case 3:
		return KeyMsg{Type: KeyCtrlC}, true
	case 13, 10:
		return KeyMsg{Type: KeyEnter}, true
	case 27:
		second, err := reader.ReadByte()
		if err != nil {
			return KeyMsg{Type: KeyEsc}, true
		}
		if second != '[' {
			_ = reader.UnreadByte()
			return KeyMsg{Type: KeyEsc}, true
		}
		third, err := reader.ReadByte()
		if err != nil {
			return KeyMsg{Type: KeyEsc}, true
		}
		switch third {
		case 'A':
			return KeyMsg{Type: KeyUp}, true
		case 'B':
			return KeyMsg{Type: KeyDown}, true
		default:
			return KeyMsg{Type: KeyEsc}, true
		}
	default:
		return KeyMsg{Type: KeyRunes, Runes: []rune{rune(b)}}, true
	}
}
