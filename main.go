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
	"crypto/tls"
	"fmt"
	"net/http"
	"os"

	"github.com/sapcc/go-bits/logg"
	apicmd "github.com/sapcc/keppel/cmd/api"
	healthmonitorcmd "github.com/sapcc/keppel/cmd/healthmonitor"
	janitorcmd "github.com/sapcc/keppel/cmd/janitor"
	validatecmd "github.com/sapcc/keppel/cmd/validate"
	"github.com/spf13/cobra"

	//include all known driver implementations
	_ "github.com/sapcc/keppel/internal/drivers/basic"
	_ "github.com/sapcc/keppel/internal/drivers/openstack"
	_ "github.com/sapcc/keppel/internal/drivers/redis"
	_ "github.com/sapcc/keppel/internal/drivers/trivial"
	"github.com/sapcc/keppel/internal/keppel"
)

func main() {
	logg.ShowDebug = keppel.MustParseBool(os.Getenv("KEPPEL_DEBUG"))

	//The KEPPEL_INSECURE flag can be used to get Keppel to work through
	//mitmproxy (which is very useful for development and debugging). (It's very
	//important that this is not the standard "KEPPEL_DEBUG" variable. That one
	//is meant to be useful for production systems, where you definitely don't
	//want to turn off certificate verification.)
	if keppel.MustParseBool(os.Getenv("KEPPEL_INSECURE")) {
		http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{
			InsecureSkipVerify: true,
		}
		http.DefaultClient.Transport = userAgentInjector{http.DefaultTransport}
	}

	rootCmd := &cobra.Command{
		Use:     "keppel",
		Short:   "Multi-tenant Docker registry",
		Long:    "Keppel is a multi-tenant Docker registry. This binary contains both the server and client implementation.",
		Version: keppel.Version,
		Args:    cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Help()
		},
	}
	validatecmd.AddCommandTo(rootCmd)

	serverCmd := &cobra.Command{
		Use:   "server",
		Short: "Server commands.",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Help()
		},
	}
	apicmd.AddCommandTo(serverCmd)
	healthmonitorcmd.AddCommandTo(serverCmd)
	janitorcmd.AddCommandTo(serverCmd)
	rootCmd.AddCommand(serverCmd)

	if err := rootCmd.Execute(); err != nil {
		logg.Fatal(err.Error())
	}
}

type userAgentInjector struct {
	Inner http.RoundTripper
}

//RoundTrip implements the http.RoundTripper interface.
func (uai userAgentInjector) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("User-Agent", fmt.Sprintf("%s/%s", keppel.Component, keppel.Version))
	return uai.Inner.RoundTrip(req)
}
