/*******************************************************************************
*
* Copyright 2020 SAP SE
*
* Licensed under the Apache License, Version 2.0 (the "License");
* you may not use this file except in compliance with the License.
* You should have received a copy of the License along with this
* program. If not, you may obtain a copy of the License at
*
*     http://www.apache.org/licenses/LICENSE-2.0
*
* Unless required by applicable law or agreed to in writing, software
* distributed under the License is distributed on an "AS IS" BASIS,
* WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
* See the License for the specific language governing permissions and
* limitations under the License.
*
*******************************************************************************/

package anycastmonitorcmd

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sapcc/go-bits/httpee"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/keppel/internal/client"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/spf13/cobra"
)

var longDesc = strings.TrimSpace(`
Monitors the accessibility of peers' healthcheck accounts on this Keppel instance.
Anycast must be enabled for this fleet of Keppel instances with the scheme and
domain name given as the first argument (e.g. "https://registry.example.com").
For each peer, the respective healthcheck account name must be given as an
additional command-line argument.

Since anycast health checks use anonymous pull access, no credentials are required.
`)

var listenAddress string

var resultGaugeVec = prometheus.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "keppel_anycastmonitor_result",
		Help: "Result from the keppel anycastmonitor check.",
	},
	[]string{"account"},
)

//AddCommandTo mounts this command into the command hierarchy.
func AddCommandTo(parent *cobra.Command) {
	cmd := &cobra.Command{
		Use:   "anycastmonitor <anycast-url> <peer=account>...",
		Short: "Monitors the accessibility of a fleet of Keppel instances over the local anycast.",
		Long:  longDesc,
		Args:  cobra.MinimumNArgs(2),
		Run:   run,
	}
	cmd.PersistentFlags().StringVar(&listenAddress, "listen", ":8080", "Listen address for Prometheus metrics endpoint")
	parent.AddCommand(cmd)
}

type anycastMonitorJob struct {
	RepoClients map[string]*client.RepoClient //key = account name
}

func run(cmd *cobra.Command, args []string) {
	keppel.Component = "keppel-anycast-health-monitor"
	prometheus.MustRegister(resultGaugeVec)

	anycastURL, err := url.Parse(args[0])
	if err != nil {
		logg.Fatal("cannot parse URL %q: %s", args[0], err)
	}

	job := &anycastMonitorJob{
		RepoClients: make(map[string]*client.RepoClient),
	}
	for _, accountName := range args[1:] {
		job.RepoClients[accountName] = &client.RepoClient{
			Scheme:   anycastURL.Scheme,
			Host:     anycastURL.Host,
			RepoName: accountName + "/healthcheck",
		}
	}

	//expose metrics endpoint
	http.Handle("/metrics", promhttp.Handler())
	ctx := httpee.ContextWithSIGINT(context.Background())
	go func() {
		logg.Info("listening on %s...", listenAddress)
		err := httpee.ListenAndServeContext(ctx, listenAddress, nil)
		if err != nil {
			logg.Fatal("error returned from httpee.ListenAndServeContext(): %s", err.Error())
		}
	}()

	//enter long-running check loop
	job.ValidateImages("latest") //once immediately to initialize the metrics
	tick := time.Tick(30 * time.Second)
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick:
			job.ValidateImages("latest")
		}
	}
}

//Validates the uploaded images and emits the keppel_anycastmonitor_result metric accordingly.
func (j *anycastMonitorJob) ValidateImages(manifestRef string) {
	for accountName, repoClient := range j.RepoClients {
		labels := prometheus.Labels{"account": accountName}
		err := repoClient.ValidateManifest(manifestRef, nil)
		if err == nil {
			resultGaugeVec.With(labels).Set(1)
		} else {
			resultGaugeVec.With(labels).Set(0)
			imageRef := client.ImageReference{
				Host:      repoClient.Host,
				RepoName:  repoClient.RepoName,
				Reference: manifestRef,
			}
			logg.Error("validation of %s failed: %s", imageRef.String(), err.Error())
		}
	}
}
