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
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sapcc/go-api-declarations/bininfo"
	"github.com/sapcc/go-bits/httpext"
	"github.com/sapcc/go-bits/logg"
	"github.com/spf13/cobra"

	"github.com/sapcc/keppel/internal/auth"
	"github.com/sapcc/keppel/internal/client"
	"github.com/sapcc/keppel/internal/keppel"
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

var anycastmonitorResultGaugeVec = prometheus.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "keppel_anycastmonitor_result",
		Help: "Healthcheck result: Whether we can pull from the given account via the anycast endpoint.",
	},
	[]string{"account"},
)

var anycastmonitorMemberGauge = prometheus.NewGauge(
	prometheus.GaugeOpts{
		Name: "keppel_anycastmonitor_membership",
		Help: "Healthcheck result: Whether this Keppel is reachable via the anycast endpoint. Reachability is proven by obtaining a token and seeing that it was issued by ourselves.",
	},
)

// AddCommandTo mounts this command into the command hierarchy.
func AddCommandTo(parent *cobra.Command) {
	cmd := &cobra.Command{
		Use:   "anycastmonitor <anycast-url> <api-public-hostname> <peer=account>...",
		Short: "Monitors the accessibility of a fleet of Keppel instances over the local anycast.",
		Long:  longDesc,
		Args:  cobra.MinimumNArgs(3),
		Run:   run,
	}
	cmd.PersistentFlags().StringVarP(&listenAddress, "listen", "l", ":8080", "Listen address for Prometheus metrics endpoint")
	parent.AddCommand(cmd)
}

type anycastMonitorJob struct {
	RepoClients map[string]*client.RepoClient //key = account name
}

func run(cmd *cobra.Command, args []string) {
	bininfo.SetTaskName("anycast-health-monitor")
	prometheus.MustRegister(anycastmonitorResultGaugeVec)
	prometheus.MustRegister(anycastmonitorMemberGauge)

	anycastURL, err := url.Parse(args[0])
	if err != nil {
		logg.Fatal("cannot parse URL %q: %s", args[0], err)
	}

	apiPublicHostname := args[1]

	job := &anycastMonitorJob{
		RepoClients: make(map[string]*client.RepoClient),
	}
	for _, accountName := range args[2:] {
		job.RepoClients[accountName] = &client.RepoClient{
			Scheme:   anycastURL.Scheme,
			Host:     anycastURL.Host,
			RepoName: accountName + "/healthcheck",
		}
	}

	//expose metrics endpoint
	http.Handle("/metrics", promhttp.Handler())
	ctx := httpext.ContextWithSIGINT(context.Background(), 1*time.Second)
	go func() {
		logg.Info("listening on %s...", listenAddress)
		err := httpext.ListenAndServeContext(ctx, listenAddress, nil)
		if err != nil {
			logg.Fatal("error returned from httpext.ListenAndServeContext(): %s", err.Error())
		}
	}()

	//enter long-running check loop
	manifestRef := keppel.ManifestReference{Tag: "latest"}
	job.ValidateImages(manifestRef) //once immediately to initialize the metrics
	job.ValidateAnycastMembership(anycastURL, apiPublicHostname)
	tick := time.Tick(30 * time.Second)
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick:
			job.ValidateImages(manifestRef)
			job.ValidateAnycastMembership(anycastURL, apiPublicHostname)
		}
	}
}

// Validates the uploaded images and emits the keppel_anycastmonitor_result metric accordingly.
func (j *anycastMonitorJob) ValidateImages(manifestRef keppel.ManifestReference) {
	for accountName, repoClient := range j.RepoClients {
		labels := prometheus.Labels{"account": accountName}
		err := repoClient.ValidateManifest(manifestRef, nil, nil)
		if err == nil {
			anycastmonitorResultGaugeVec.With(labels).Set(1)
		} else {
			anycastmonitorResultGaugeVec.With(labels).Set(0)
			imageRef := keppel.ImageReference{
				Host:      repoClient.Host,
				RepoName:  repoClient.RepoName,
				Reference: manifestRef,
			}
			logg.Error("validation of %s failed: %s", imageRef.String(), err.Error())
		}
	}
}

func checkAnycastMembership(anycastURL *url.URL, apiPublicHostname string) (bool, error) {
	resp, err := http.Get(fmt.Sprintf("%s://%s/keppel/v1/auth?service=%[2]s&scope=repository:foo/bar:pull", anycastURL.Scheme, anycastURL.Host))
	if err != nil {
		return false, fmt.Errorf("failed getting anon token: %s", err.Error())
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, fmt.Errorf("failed reading body: %s", err.Error())
	}

	var data auth.TokenResponse
	err = json.Unmarshal(body, &data)
	if err != nil {
		return false, fmt.Errorf("failed to unmarshal JWT: %s", err.Error())
	}
	jwtToken := strings.SplitN(data.Token, ".", 3)
	if len(jwtToken) != 3 {
		return false, fmt.Errorf("jwtToken contains not enough section separated by .: %s", jwtToken)
	}
	token, err := base64.StdEncoding.DecodeString(jwtToken[1])
	if err != nil {
		return false, fmt.Errorf("failed to decode claim from token %s: %s", token, err.Error())
	}
	var tokenJSON struct {
		Issuer string `json:"iss"`
	}
	err = json.Unmarshal(token, &tokenJSON)
	if err != nil {
		return false, fmt.Errorf("failed to unmarshal claim from token %s: %s", token, err.Error())
	}

	expectedIssuer := fmt.Sprintf("keppel-api@%s", apiPublicHostname)
	if tokenJSON.Issuer != expectedIssuer {
		return false, fmt.Errorf("anycast membership wrong: expected %s, got %s", expectedIssuer, tokenJSON.Issuer)
	}
	return tokenJSON.Issuer == expectedIssuer, nil
}

func (j *anycastMonitorJob) ValidateAnycastMembership(anycastURL *url.URL, apiPublicHostname string) {
	isAnycastMember, err := checkAnycastMembership(anycastURL, apiPublicHostname)

	if isAnycastMember && err == nil {
		anycastmonitorMemberGauge.Set(1)
	} else {
		anycastmonitorMemberGauge.Set(0)
		if err != nil {
			logg.Error("member check failed: %s", err.Error())
		}
	}
}
