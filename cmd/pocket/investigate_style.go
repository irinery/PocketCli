package main

import (
	"io"
	"os"

	"golang.org/x/term"
)

// palette colors investigate output only on an interactive terminal, and
// honors NO_COLOR (https://no-color.org) and TERM=dumb. Logs, pipes and CI
// keep plain text.
type palette struct{ on bool }

func newPalette(out io.Writer) palette {
	file, ok := out.(*os.File)
	return palette{on: ok && term.IsTerminal(int(file.Fd())) && os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"}
}

func (p palette) paint(code, text string) string {
	if !p.on || text == "" {
		return text
	}
	return "\x1b[" + code + "m" + text + "\x1b[0m"
}

func (p palette) bold(text string) string   { return p.paint("1", text) }
func (p palette) dim(text string) string    { return p.paint("2", text) }
func (p palette) red(text string) string    { return p.paint("31", text) }
func (p palette) green(text string) string  { return p.paint("32", text) }
func (p palette) yellow(text string) string { return p.paint("33", text) }
func (p palette) cyan(text string) string   { return p.paint("36", text) }
