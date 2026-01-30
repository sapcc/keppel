// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"github.com/sapcc/go-api-declarations/bininfo"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/go-bits/must"
	"github.com/sapcc/go-bits/osext"
	"github.com/spf13/cobra"

	anycastmonitorcmd "github.com/sapcc/keppel/cmd/anycastmonitor"
	apicmd "github.com/sapcc/keppel/cmd/api"
	healthmonitorcmd "github.com/sapcc/keppel/cmd/healthmonitor"
	janitorcmd "github.com/sapcc/keppel/cmd/janitor"
	testcmd "github.com/sapcc/keppel/cmd/test"
	trivyproxycmd "github.com/sapcc/keppel/cmd/trivyproxy"
	validatecmd "github.com/sapcc/keppel/cmd/validate"
	validateconfigcmd "github.com/sapcc/keppel/cmd/validateconfig"
	"github.com/sapcc/keppel/internal/keppel"

	// include all known driver implementations
	_ "github.com/sapcc/keppel/internal/drivers/basic"
	_ "github.com/sapcc/keppel/internal/drivers/filesystem"
	_ "github.com/sapcc/keppel/internal/drivers/multi"
	_ "github.com/sapcc/keppel/internal/drivers/openstack"
	_ "github.com/sapcc/keppel/internal/drivers/redis"
	_ "github.com/sapcc/keppel/internal/drivers/trivial"
)

func main() {
	logg.ShowDebug = osext.GetenvBool("KEPPEL_DEBUG")
	keppel.SetupHTTPClient()

	rootCmd := &cobra.Command{
		Use:     "keppel",
		Short:   "Multi-tenant Docker registry",
		Long:    "Keppel is a multi-tenant Docker registry. This binary contains both the server and client implementation.",
		Version: bininfo.VersionOr("unknown"), // returning empty string here hides the flag from cobra completely
		Args:    cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Help()
		},
	}
	validatecmd.AddCommandTo(rootCmd)
	testcmd.AddCommandTo(rootCmd)

	serverCmd := &cobra.Command{
		Use:   "server <subcommand> <args...>",
		Short: "Server commands.",
		Args:  cobra.MinimumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Help()
		},
	}
	anycastmonitorcmd.AddCommandTo(serverCmd)
	apicmd.AddCommandTo(serverCmd)
	healthmonitorcmd.AddCommandTo(serverCmd)
	janitorcmd.AddCommandTo(serverCmd)
	trivyproxycmd.AddCommandTo(serverCmd)
	validateconfigcmd.AddCommandTo(serverCmd)
	rootCmd.AddCommand(serverCmd)

	must.Succeed(rootCmd.Execute())
}
