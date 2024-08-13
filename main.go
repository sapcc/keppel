/******************************************************************************
*
*  Copyright 2020 SAP SE
*
*  Licensed under the Apache License, Version 2.0 (the "License");
*  you may not use this file except in compliance with the License.
*  You may obtain a copy of the License at
*
*      http://www.apache.org/licenses/LICENSE-2.0
*
*  Unless required by applicable law or agreed to in writing, software
*  distributed under the License is distributed on an "AS IS" BASIS,
*  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
*  See the License for the specific language governing permissions and
*  limitations under the License.
*
******************************************************************************/

package main

import (
	"github.com/sapcc/go-api-declarations/bininfo"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/go-bits/must"
	"github.com/sapcc/go-bits/osext"
	"github.com/spf13/cobra"
	"go.uber.org/automaxprocs/maxprocs"

	anycastmonitorcmd "github.com/sapcc/keppel/cmd/anycastmonitor"
	apicmd "github.com/sapcc/keppel/cmd/api"
	healthmonitorcmd "github.com/sapcc/keppel/cmd/healthmonitor"
	janitorcmd "github.com/sapcc/keppel/cmd/janitor"
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
	undoMaxprocs := must.Return(maxprocs.Set(maxprocs.Logger(logg.Debug)))
	defer undoMaxprocs()
	keppel.SetupHTTPClient()

	rootCmd := &cobra.Command{
		Use:     "keppel",
		Short:   "Multi-tenant Docker registry",
		Long:    "Keppel is a multi-tenant Docker registry. This binary contains both the server and client implementation.",
		Version: bininfo.Version(),
		Args:    cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Help()
		},
	}
	validatecmd.AddCommandTo(rootCmd)

	serverCmd := &cobra.Command{
		Use:   "server <subcommand> <args...>",
		Short: "Server commands.",
		Args:  cobra.NoArgs,
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
