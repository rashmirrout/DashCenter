package cmd

import (
	"github.com/spf13/cobra"
)

func (a *Application) newCompletionCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "completion <bash|zsh|fish|powershell>",
		Short: "Generate shell completion script",
		Long: `To load completions for the current session:

  bash:        source <(dashctl completion bash)
  zsh:         dashctl completion zsh  > "${fpath[1]}/_dashctl"
  fish:        dashctl completion fish | source
  powershell:  dashctl completion powershell | Out-String | Invoke-Expression
`,
		ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
		Args:      cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return cmd.Root().GenBashCompletionV2(a.Out, true)
			case "zsh":
				return cmd.Root().GenZshCompletion(a.Out)
			case "fish":
				return cmd.Root().GenFishCompletion(a.Out, true)
			case "powershell":
				return cmd.Root().GenPowerShellCompletionWithDesc(a.Out)
			}
			return nil
		},
	}
	return c
}
