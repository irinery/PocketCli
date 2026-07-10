package main

import (
	"fmt"

	"github.com/spf13/cobra"
	"pocketcli/internal/catalogue"
)

func newSSHForwardCommand(use string, typoAlias bool) *cobra.Command {
	cmd := &cobra.Command{
		Use:   use + " <host> <service-or-port>",
		Short: "Cria túnel SSH local com bind seguro em 127.0.0.1",
		RunE: func(cmd *cobra.Command, args []string) error {
			if typoAlias {
				fmt.Fprintln(cmd.OutOrStdout(), "WARN_FORWARD_TYPO_ALIAS: use pocket ssh.forward")
			}
			req, dryRun, err := parseSSHForwardArgs(args)
			if err != nil {
				return err
			}
			if dryRun {
				result, err := catalogue.BuildForward(req)
				if err != nil {
					return err
				}
				return writeJSON(cmd.OutOrStdout(), result)
			}
			result, err := catalogue.StartForward(req, "")
			if err != nil {
				return err
			}
			return writeJSON(cmd.OutOrStdout(), result)
		},
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "list [--json]",
		Short: "Lista forwards SSH em background",
		RunE: func(cmd *cobra.Command, args []string) error {
			jsonOut := removeBoolFlag(&args, "--json")
			if len(args) > 0 {
				return fmt.Errorf("flag inválida: %s", args[0])
			}
			sessions, err := (catalogue.ForwardStore{}).List()
			if err != nil {
				return err
			}
			if jsonOut {
				return writeJSON(cmd.OutOrStdout(), sessions)
			}
			for _, session := range sessions {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\tpid=%d\t%s:%d->%s:%d\tstatus=%s\n", session.Name, session.PID, session.Bind, session.LocalPort, session.RemoteHost, session.RemotePort, session.Status)
			}
			if len(sessions) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "nenhum forward gerenciado")
			}
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "stop <name> [--force]",
		Short: "Para forward SSH gerenciado",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			force := removeBoolFlag(&args, "--force")
			if len(args) != 1 {
				return fmt.Errorf("stop exige exatamente um nome")
			}
			session, err := (catalogue.ForwardStore{}).Stop(args[0], force)
			if err != nil {
				return err
			}
			return writeJSON(cmd.OutOrStdout(), session)
		},
	})
	return cmd
}

func parseSSHForwardArgs(args []string) (catalogue.ForwardRequest, bool, error) {
	req := catalogue.ForwardRequest{}
	dryRun := false
	positionals := make([]string, 0, 2)
	for idx := 0; idx < len(args); idx++ {
		arg := args[idx]
		switch arg {
		case "--dry-run":
			dryRun = true
		case "--background":
			req.Background = true
		case "--allow-public-bind":
			req.AllowPublicBind = true
		default:
			if arg == "--name" || arg == "--bind" || arg == "--user" {
				idx++
				if idx >= len(args) {
					return req, false, fmt.Errorf("flag %s requer valor", arg)
				}
				switch arg {
				case "--name":
					req.Name = args[idx]
				case "--bind":
					req.Bind = args[idx]
				case "--user":
					req.User = args[idx]
				}
				continue
			}
			if len(arg) > 7 && arg[:7] == "--name=" {
				req.Name = arg[7:]
				continue
			}
			if len(arg) > 7 && arg[:7] == "--bind=" {
				req.Bind = arg[7:]
				continue
			}
			if len(arg) > 7 && arg[:7] == "--user=" {
				req.User = arg[7:]
				continue
			}
			if len(arg) > 0 && arg[0] == '-' {
				return req, false, fmt.Errorf("flag inválida: %s", arg)
			}
			positionals = append(positionals, arg)
		}
	}
	if len(positionals) != 2 {
		return req, false, fmt.Errorf("uso: pocket ssh.forward <host> <service-or-port>")
	}
	req.Host = positionals[0]
	req.ServiceOrPorts = positionals[1]
	return req, dryRun, nil
}
