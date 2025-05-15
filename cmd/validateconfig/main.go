// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package validateconfigcmd

import (
	"github.com/spf13/cobra"

	"github.com/sapcc/go-bits/must"

	"github.com/sapcc/keppel/internal/drivers/basic"
)

// AddCommandTo mounts this command into the command hierarchy.
func AddCommandTo(parent *cobra.Command) {
	cmd := &cobra.Command{
		Use:   "validate-config",
		Short: "Validates driver configuration files.",
		Long: `Contains subcommands to validate configuration files for specific drivers.
This is intended to be used e.g. for preflight checks in CI deployments.`,
		Args: cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Help()
		},
	}
	parent.AddCommand(cmd)

	cmd.AddCommand(&cobra.Command{
		Use:     "account-management-basic <path>",
		Example: "  keppel server validate-config account-management-basic ./config/managed-accounts.json",
		Short:   `Validates a configuration file for the account management driver "basic".`,
		Args:    cobra.ExactArgs(1),
		Run:     runForAccountManagementBasic,
	})
}

func runForAccountManagementBasic(cmd *cobra.Command, args []string) {
	driver := &basic.AccountManagementDriver{ConfigPath: args[0]}
	must.Succeed(driver.LoadConfig())
}
