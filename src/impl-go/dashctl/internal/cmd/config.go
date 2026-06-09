package cmd

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/internal/config"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/internal/errors"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/internal/render"
)

func (a *Application) newConfigCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "config",
		Short: "Manage dashctl contexts",
	}
	c.AddCommand(
		a.newConfigViewCmd(),
		a.newConfigGetContextsCmd(),
		a.newConfigCurrentContextCmd(),
		a.newConfigUseContextCmd(),
		a.newConfigSetContextCmd(),
		a.newConfigDeleteContextCmd(),
		a.newConfigRenameContextCmd(),
	)
	return c
}

func (a *Application) loadConfigForEdit() (*config.Config, string, error) {
	path := a.Flags.configPath
	cfg, err := config.Load(path)
	if err != nil {
		return nil, "", errors.Wrap(errors.CodeInvalidArgument, "config", err)
	}
	if path == "" {
		path = config.DefaultPath()
	}
	return cfg, path, nil
}

func (a *Application) newConfigViewCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "view",
		Short: "Print the resolved configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := a.loadConfigForEdit()
			if err != nil {
				return err
			}
			return render.Render(a.Out, cfg, render.Options{Format: render.FormatYAML})
		},
	}
}

func (a *Application) newConfigGetContextsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get-contexts",
		Short: "List context names",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := a.loadConfigForEdit()
			if err != nil {
				return err
			}
			names := cfg.ContextNames()
			sort.Strings(names)
			for _, n := range names {
				marker := "  "
				if n == cfg.CurrentContext {
					marker = "* "
				}
				fmt.Fprintf(a.Out, "%s%s\n", marker, n)
			}
			return nil
		},
	}
}

func (a *Application) newConfigCurrentContextCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "current-context",
		Short: "Print the active context name",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := a.loadConfigForEdit()
			if err != nil {
				return err
			}
			fmt.Fprintln(a.Out, cfg.CurrentContext)
			return nil
		},
	}
}

func (a *Application) newConfigUseContextCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "use-context <name>",
		Short: "Set the active context",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, path, err := a.loadConfigForEdit()
			if err != nil {
				return err
			}
			if err := cfg.SetCurrentContext(args[0]); err != nil {
				return errors.Wrap(errors.CodeInvalidArgument, "config", err)
			}
			if err := config.Save(cfg, path); err != nil {
				return errors.Wrap(errors.CodeInternal, "config", err)
			}
			fmt.Fprintf(a.Out, "Switched to context %q.\n", args[0])
			return nil
		},
	}
}

func (a *Application) newConfigSetContextCmd() *cobra.Command {
	var (
		endpoint  string
		admin     string
		transport string
		namespace string
		tokenEnv  string
	)
	c := &cobra.Command{
		Use:   "set-context <name>",
		Short: "Create or update a context",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, path, err := a.loadConfigForEdit()
			if err != nil {
				return err
			}
			entry := cfg.Contexts[args[0]]
			if endpoint != "" {
				entry.Endpoint = endpoint
			}
			if admin != "" {
				entry.AdminEndpoint = admin
			}
			if transport != "" {
				entry.Transport = config.Transport(transport)
			}
			if namespace != "" {
				entry.Namespace = namespace
			}
			if tokenEnv != "" {
				entry.Auth.Mode = "token"
				entry.Auth.TokenEnv = tokenEnv
			}
			if err := cfg.PutContext(args[0], entry); err != nil {
				return errors.Wrap(errors.CodeInvalidArgument, "config", err)
			}
			if err := config.Save(cfg, path); err != nil {
				return errors.Wrap(errors.CodeInternal, "config", err)
			}
			fmt.Fprintf(a.Out, "Context %q saved.\n", args[0])
			return nil
		},
	}
	c.Flags().StringVar(&endpoint, "endpoint", "", "dashd endpoint")
	c.Flags().StringVar(&admin, "admin-endpoint", "", "admin endpoint (defaults to derived/7443)")
	c.Flags().StringVar(&transport, "transport", "", "rest|grpc")
	c.Flags().StringVar(&namespace, "namespace", "", "default namespace for this context")
	c.Flags().StringVar(&tokenEnv, "token-env", "", "env var name holding the bearer token")
	return c
}

func (a *Application) newConfigDeleteContextCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete-context <name>",
		Short: "Remove a context",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, path, err := a.loadConfigForEdit()
			if err != nil {
				return err
			}
			if err := cfg.DeleteContext(args[0]); err != nil {
				return errors.Wrap(errors.CodeInvalidArgument, "config", err)
			}
			if err := config.Save(cfg, path); err != nil {
				return errors.Wrap(errors.CodeInternal, "config", err)
			}
			fmt.Fprintf(a.Out, "Context %q deleted.\n", args[0])
			return nil
		},
	}
}

func (a *Application) newConfigRenameContextCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rename-context <old> <new>",
		Short: "Rename a context",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, path, err := a.loadConfigForEdit()
			if err != nil {
				return err
			}
			if err := cfg.RenameContext(args[0], args[1]); err != nil {
				return errors.Wrap(errors.CodeInvalidArgument, "config", err)
			}
			if err := config.Save(cfg, path); err != nil {
				return errors.Wrap(errors.CodeInternal, "config", err)
			}
			fmt.Fprintf(a.Out, "Context %q → %q.\n", args[0], args[1])
			return nil
		},
	}
}
