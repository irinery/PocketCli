package cobra

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

type PositionalArgs func(cmd *Command, args []string) error

type Command struct {
	Use    string
	Short  string
	Hidden bool
	Args   PositionalArgs
	RunE   func(cmd *Command, args []string) error

	parent   *Command
	children []*Command
	out      io.Writer
}

func (c *Command) AddCommand(cmds ...*Command) {
	for _, child := range cmds {
		child.parent = c
		c.children = append(c.children, child)
	}
}

func (c *Command) Execute() error {
	args := os.Args[1:]
	return c.executeArgs(args)
}

func (c *Command) executeArgs(args []string) error {
	if len(args) > 0 {
		for _, child := range c.children {
			if child.commandName() == args[0] {
				return child.executeArgs(args[1:])
			}
		}
	}

	if c.Args != nil {
		if err := c.Args(c, args); err != nil {
			return err
		}
	}

	if c.RunE != nil {
		return c.RunE(c, args)
	}

	return c.Help()
}

func (c *Command) commandName() string {
	parts := strings.Fields(c.Use)
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}

func (c *Command) OutOrStdout() io.Writer {
	if c.out != nil {
		return c.out
	}
	if c.parent != nil {
		return c.parent.OutOrStdout()
	}
	return os.Stdout
}

func (c *Command) Help() error {
	w := c.OutOrStdout()
	fmt.Fprintf(w, "%s\n", c.Short)
	fmt.Fprintf(w, "Usage: %s\n", c.Use)
	if len(c.children) > 0 {
		fmt.Fprintln(w, "\nCommands:")
		for _, child := range c.children {
			if child.Hidden {
				continue
			}
			fmt.Fprintf(w, "  %s\t%s\n", child.commandName(), child.Short)
		}
	}
	return nil
}

func ExactArgs(n int) PositionalArgs {
	return func(cmd *Command, args []string) error {
		if len(args) != n {
			return fmt.Errorf("accepts %d arg(s), received %d", n, len(args))
		}
		return nil
	}
}

func MinimumNArgs(n int) PositionalArgs {
	return func(cmd *Command, args []string) error {
		if len(args) < n {
			return fmt.Errorf("requires at least %d arg(s), received %d", n, len(args))
		}
		return nil
	}
}

var ErrSubCommandRequired = errors.New("subcommand required")
