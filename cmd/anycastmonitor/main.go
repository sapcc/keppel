// SPDX-FileCopyrightText: 2020 SAP SE
// SPDX-License-Identifier: Apache-2.0

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
	"github.com/sapcc/go-bits/httpext"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/go-bits/must"
	"github.com/spf13/cobra"

	"github.com/sapcc/keppel/internal/auth"
	"github.com/sapcc/keppel/internal/client"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
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
	RepoClients map[string]*client.RepoClient // key = account name
}

func run(cmd *cobra.Command, args []string) {
	keppel.SetTaskName("anycast-health-monitor")
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

	// expose metrics endpoint
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	ctx := httpext.ContextWithSIGINT(cmd.Context(), 1*time.Second)
	go func() {
		must.Succeed(httpext.ListenAndServeContext(ctx, listenAddress, mux))
	}()

	// enter long-running check loop
	manifestRef := models.ManifestReference{Tag: "latest"}
	job.ValidateImages(ctx, manifestRef) // once immediately to initialize the metrics
	job.ValidateAnycastMembership(ctx, anycastURL, apiPublicHostname)
	tick := time.Tick(30 * time.Second)
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick:
			job.ValidateImages(ctx, manifestRef)
			job.ValidateAnycastMembership(ctx, anycastURL, apiPublicHostname)
		}
	}
}

// Validates the uploaded images and emits the keppel_anycastmonitor_result metric accordingly.
func (j *anycastMonitorJob) ValidateImages(ctx context.Context, manifestRef models.ManifestReference) {
	for accountName, repoClient := range j.RepoClients {
		labels := prometheus.Labels{"account": accountName}
		err := repoClient.ValidateManifest(ctx, manifestRef, nil, nil)
		if err == nil {
			anycastmonitorResultGaugeVec.With(labels).Set(1)
		} else {
			anycastmonitorResultGaugeVec.With(labels).Set(0)
			imageRef := models.ImageReference{
				Host:      repoClient.Host,
				RepoName:  repoClient.RepoName,
				Reference: manifestRef,
			}
			logg.Error("validation of %s failed: %s", imageRef, err.Error())
		}
	}
}

func checkAnycastMembership(ctx context.Context, anycastURL *url.URL, apiPublicHostname string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s://%s/keppel/v1/auth?service=%[2]s&scope=repository:foo/bar:pull", anycastURL.Scheme, anycastURL.Host), http.NoBody)
	if err != nil {
		return false, fmt.Errorf("failed creating request: %s", err.Error())
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("failed getting anon token: %s", err.Error())
	}
	defer resp.Body.Close()
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

	expectedIssuer := "keppel-api@" + apiPublicHostname
	if tokenJSON.Issuer != expectedIssuer {
		return false, fmt.Errorf("anycast membership wrong: expected %s, got %s", expectedIssuer, tokenJSON.Issuer)
	}
	return tokenJSON.Issuer == expectedIssuer, nil
}

func (j *anycastMonitorJob) ValidateAnycastMembership(ctx context.Context, anycastURL *url.URL, apiPublicHostname string) {
	isAnycastMember, err := checkAnycastMembership(ctx, anycastURL, apiPublicHostname)

	if isAnycastMember && err == nil {
		anycastmonitorMemberGauge.Set(1)
	} else {
		anycastmonitorMemberGauge.Set(0)
		if err != nil {
			logg.Error("member check failed: %s", err.Error())
		}
	}
}
