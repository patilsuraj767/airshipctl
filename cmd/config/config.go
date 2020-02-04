package config

import (
	"github.com/spf13/cobra"

	"opendev.org/airship/airshipctl/pkg/environment"
)

// NewConfigCommand creates a command object for the airshipctl "config" , and adds all child commands to it.
func NewConfigCommand(rootSettings *environment.AirshipCTLSettings) *cobra.Command {
	configRootCmd := &cobra.Command{
		Use:                   "config",
		DisableFlagsInUseLine: true,
		Short:                 ("Modify airshipctl config files"),
		Long: (`Modify airshipctl config files using subcommands
like "airshipctl config set-context --current-context my-context" `),
	}
	configRootCmd.AddCommand(NewCmdConfigSetCluster(rootSettings))
	configRootCmd.AddCommand(NewCmdConfigGetCluster(rootSettings))
	configRootCmd.AddCommand(NewCmdConfigSetContext(rootSettings))
	configRootCmd.AddCommand(NewCmdConfigGetContext(rootSettings))
	configRootCmd.AddCommand(NewCmdConfigInit(rootSettings))
	configRootCmd.AddCommand(NewCmdConfigSetAuthInfo(rootSettings))
	configRootCmd.AddCommand(NewCmdConfigGetAuthInfo(rootSettings))

	return configRootCmd
}
