/******************************************************************************
*
*  Copyright 2023 SAP SE
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

package trivyproxycmd

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/trivy"

	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sapcc/go-bits/httpapi"
	"github.com/sapcc/go-bits/httpext"
	"github.com/sapcc/go-bits/must"
	"github.com/sapcc/go-bits/osext"
	"github.com/spf13/cobra"
	"golang.org/x/exp/slices"
)

func AddCommandTo(parent *cobra.Command) {
	cmd := &cobra.Command{
		Use:     "trivy-proxy",
		Example: "  keppel trivy-proxy",
		Short:   "Starts a web server which offers the trivy proxy API",
		Long: `Starts a web server which offers the trivy proxy API.
The proxy server is going to exec the trivy binary and connecting with to a trivy running in server mode.
The token is used to both authenticate API requests to the proxy, as well to authenticate to the triv server`,
		Run: run,
	}
	parent.AddCommand(cmd)
}

func run(cmd *cobra.Command, args []string) {
	keppel.SetTaskName("trivy")

	ctx := httpext.ContextWithSIGINT(context.Background(), 10*time.Second)

	token := osext.MustGetenv("KEPPEL_TRIVY_TOKEN")
	dbMirrorPrefix := osext.MustGetenv("KEPPEL_TRIVY_DB_MIRROR_PREFIX")
	trivyURL := osext.MustGetenv("KEPPEL_TRIVY_URL")

	handler := httpapi.Compose(
		NewAPI(dbMirrorPrefix, token, trivyURL),
		httpapi.HealthCheckAPI{SkipRequestLog: true},
	)
	http.Handle("/", handler)
	http.Handle("/metrics", promhttp.Handler())

	apiListenAddress := osext.GetenvOrDefault("KEPPEL_API_LISTEN_ADDRESS", ":8080")
	must.Succeed(httpext.ListenAndServeContext(ctx, apiListenAddress, nil))
}

// API contains state variables used by the Trivy API proxy.
type API struct {
	dbMirrorPrefix string
	token          string
	trivyURL       string
}

// NewAPI constructs a new API instance.
func NewAPI(dbMirrorPrefix, token, trivyURL string) *API {
	return &API{
		dbMirrorPrefix: dbMirrorPrefix,
		token:          token,
		trivyURL:       trivyURL,
	}
}

// AddTo implements the api.API interface.
func (a *API) AddTo(r *mux.Router) {
	r.Methods("GET").Path("/trivy").HandlerFunc(a.proxyToTrivy)
}

func (a *API) proxyToTrivy(w http.ResponseWriter, r *http.Request) {
	httpapi.IdentifyEndpoint(r, "/trivy")

	secretHeader := r.Header[http.CanonicalHeaderKey(trivy.TokenHeader)]
	if !slices.Contains(secretHeader, a.token) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	query := r.URL.Query()
	imageURL := query.Get("image")
	if imageURL == "" {
		http.Error(w, "image query string must be supplied and cannot be empty", http.StatusUnprocessableEntity)
		return
	}

	format := query.Get("format")
	if format == "" {
		format = "json"
	}

	keppelToken := r.Header.Get(trivy.KeppelTokenHeader)

	stdout, stderr, err := a.runTrivy(r.Context(), imageURL, format, keppelToken)
	if err != nil {
		cleanedErr := strings.ReplaceAll(strings.TrimSpace(string(stderr)), "\n", " ")
		http.Error(w, fmt.Sprintf("trivy: %s: %s", err, cleanedErr), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")
	w.Write(stdout)
}

func (a *API) runTrivy(ctx context.Context, imageURL, format, keppelToken string) (stdout, stderr []byte, err error) {
	//nolint:gosec //intented behaviour
	cmd := exec.CommandContext(ctx,
		"trivy", "image",
		"--scanners", "vuln",
		"--skip-db-update",
		// remove when https://github.com/aquasecurity/trivy/issues/3560 is resolved
		"--java-db-repository", a.dbMirrorPrefix+"/aquasecurity/trivy-java-db",
		"--server", a.trivyURL,
		"--registry-token", keppelToken,
		"--format", format,
		"--token", a.token,
		"--timeout", "10m", // default is 5m
		"--image-src", "remote", // don't try to use a container runtime which is not installed anyway
		imageURL)
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf
	err = cmd.Run()

	return stdoutBuf.Bytes(), stderrBuf.Bytes(), err
}
