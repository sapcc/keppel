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

package healthmonitorcmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
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
Monitors the health of a Keppel instance. This sets up a Keppel account with
the given name containing a single image, and pulls the image at regular
intervals. The health check result will be published as a Prometheus metric.

The environment variables must contain credentials for authenticating with the
authentication method used by the target Keppel API.
`)

var listenAddress string

var healthmonitorResultGauge = prometheus.NewGauge(
	prometheus.GaugeOpts{
		Name: "keppel_healthmonitor_result",
		Help: "Result from the keppel healthmonitor check.",
	},
)

//AddCommandTo mounts this command into the command hierarchy.
func AddCommandTo(parent *cobra.Command) {
	cmd := &cobra.Command{
		Use:   "healthmonitor <account>",
		Short: "Monitors the health of a Keppel instance.",
		Long:  longDesc,
		Args:  cobra.ExactArgs(1),
		Run:   run,
	}
	cmd.PersistentFlags().StringVar(&listenAddress, "listen", ":8080", "Listen address for Prometheus metrics endpoint")
	parent.AddCommand(cmd)
}

type healthMonitorJob struct {
	AuthDriver  client.AuthDriver
	AccountName string
	RepoClient  *client.RepoClient

	LastResultLock *sync.RWMutex
	LastResult     *bool //nil during initialization, non-nil indicates result of last healthcheck
}

func run(cmd *cobra.Command, args []string) {
	keppel.Component = "keppel-health-monitor"
	prometheus.MustRegister(healthmonitorResultGauge)

	ad, err := client.NewAuthDriver()
	if err != nil {
		logg.Fatal("while setting up auth driver: %s", err.Error())
	}

	apiUser, apiPassword := ad.CredentialsForRegistryAPI()
	job := &healthMonitorJob{
		AuthDriver:  ad,
		AccountName: args[0],
		RepoClient: &client.RepoClient{
			Scheme:   ad.ServerScheme(),
			Host:     ad.ServerHost(),
			RepoName: args[0] + "/healthcheck",
			UserName: apiUser,
			Password: apiPassword,
		},
		LastResultLock: &sync.RWMutex{},
	}

	//run one-time preparations
	err = job.PrepareKeppelAccount()
	if err != nil {
		logg.Fatal("while preparing Keppel account: %s", err.Error())
	}
	manifestRef, err := job.UploadImage()
	if err != nil {
		logg.Fatal("while uploading test image: %s", err.Error())
	}

	//expose metrics endpoint
	http.HandleFunc("/healthcheck", job.ReportHealthcheckResult)
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
	job.ValidateImage(manifestRef) //once immediately to initialize the metric
	tick := time.Tick(30 * time.Second)
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick:
			job.ValidateImage(manifestRef)
		}
	}
}

//Creates the Keppel account for this job if it does not exist yet.
func (j *healthMonitorJob) PrepareKeppelAccount() error {
	reqBody := map[string]interface{}{
		"account": map[string]interface{}{
			"auth_tenant_id": j.AuthDriver.CurrentAuthTenantID(),
			//anonymous pull access is needed for `keppel server anycastmonitor`
			"rbac_policies": []map[string]interface{}{{
				"match_repository": "healthcheck",
				"permissions":      []string{"anonymous_pull"},
			}},
		},
	}
	reqBodyBytes, _ := json.Marshal(reqBody)

	req, err := http.NewRequest("PUT", "/keppel/v1/accounts/"+j.AccountName, bytes.NewReader(reqBodyBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := j.AuthDriver.SendHTTPRequest(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

//Uploads a minimal complete image (one blob and one manifest) for testing.
func (j *healthMonitorJob) UploadImage() (string, error) {
	_, err := j.RepoClient.UploadMonolithicBlob([]byte(minimalImageConfiguration))
	if err != nil {
		return "", err
	}
	digest, err := j.RepoClient.UploadManifest([]byte(minimalManifest), minimalManifestMediaType, "latest")
	return digest.String(), err
}

//Validates the uploaded image and emits the keppel_healthmonitor_result metric accordingly.
func (j *healthMonitorJob) ValidateImage(manifestRef string) {
	err := j.RepoClient.ValidateManifest(manifestRef, nil)
	if err == nil {
		j.recordHealthcheckResult(true)
	} else {
		j.recordHealthcheckResult(false)
		imageRef := client.ImageReference{
			Host:      j.RepoClient.Host,
			RepoName:  j.RepoClient.RepoName,
			Reference: manifestRef,
		}
		logg.Error("validation of %s failed: %s", imageRef.String(), err.Error())
	}
}

func (j *healthMonitorJob) recordHealthcheckResult(ok bool) {
	if ok {
		healthmonitorResultGauge.Set(1)
	} else {
		healthmonitorResultGauge.Set(0)
	}
	j.LastResultLock.Lock()
	j.LastResult = &ok
	j.LastResultLock.Unlock()
}

//Provides the GET /healthcheck endpoint.
func (j *healthMonitorJob) ReportHealthcheckResult(w http.ResponseWriter, r *http.Request) {
	j.LastResultLock.RLock()
	lastResult := j.LastResult
	j.LastResultLock.RUnlock()

	if lastResult == nil {
		http.Error(w, "still starting up", http.StatusServiceUnavailable)
	} else if *lastResult {
		w.WriteHeader(http.StatusNoContent)
	} else {
		http.Error(w, "healthcheck failed", http.StatusInternalServerError)
	}
}
