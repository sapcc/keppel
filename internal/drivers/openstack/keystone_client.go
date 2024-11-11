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

package openstack

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/identity/v3/tokens"

	"github.com/sapcc/go-bits/gophercloudext"
	"github.com/sapcc/keppel/internal/client"
)

type keystoneClientDriver struct {
	Client           *gophercloud.ServiceClient
	CurrentProjectID string
	RegistryUserName string
	RegistryPassword string
}

func init() {
	client.RegisterAuthDriver("keystone", func() client.AuthDriver {
		return &keystoneClientDriver{}
	})
}

// MatchesEnvironment implements the client.AuthDriver interface.
func (d *keystoneClientDriver) MatchesEnvironment() bool {
	return os.Getenv("OS_AUTH_URL") != ""
}

// Connect implements the client.AuthDriver interface.
func (d *keystoneClientDriver) Connect(ctx context.Context) error {
	var ao gophercloud.AuthOptions

	provider, eo, err := gophercloudext.NewProviderClient(ctx, &gophercloudext.ClientOpts{
		CustomizeAuthOptions: func(opts *gophercloud.AuthOptions) {
			// we don't customize anything, but we need a copy for the logic below
			ao = *opts
		},
	})
	if err != nil {
		return err
	}
	eo.ApplyDefaults("keppel")
	endpointURL, err := provider.EndpointLocator(eo)
	if err != nil {
		return errors.New("cannot find Keppel service URL: " + err.Error())
	}
	d.Client = &gophercloud.ServiceClient{
		ProviderClient: provider,
		Endpoint:       endpointURL,
		Type:           "keppel",
	}

	authResult, ok := provider.GetAuthResult().(tokens.CreateResult)
	if !ok {
		return fmt.Errorf("got unexpected auth result: %T", provider.GetAuthResult())
	}
	project, err := authResult.ExtractProject()
	if err != nil {
		return errors.New("cannot extract project scope from token response: " + err.Error())
	}
	d.CurrentProjectID = project.ID

	if ao.ApplicationCredentialID != "" && ao.ApplicationCredentialSecret != "" {
		d.RegistryUserName = "applicationcredential-" + ao.ApplicationCredentialID
		d.RegistryPassword = ao.ApplicationCredentialSecret
	} else {
		user, err := authResult.ExtractUser()
		if err != nil {
			return errors.New("cannot extract project scope from token response: " + err.Error())
		}
		d.RegistryUserName = fmt.Sprintf("%s@%s/%s@%s",
			user.Name, user.Domain.Name,
			project.Name, project.Domain.Name,
		)
		d.RegistryPassword = ao.Password
	}

	return nil
}

// CurrentAuthTenantID implements the client.AuthDriver interface.
func (d *keystoneClientDriver) CurrentAuthTenantID() string {
	return d.CurrentProjectID
}

// ServerHost implements the client.AuthDriver interface.
func (d *keystoneClientDriver) ServerHost() string {
	if d.Client == nil {
		panic("called before Connect()")
	}
	endpointURL, err := url.Parse(d.Client.Endpoint)
	if err == nil {
		return endpointURL.Host
	}
	return ""
}

// ServerScheme implements the client.AuthDriver interface.
func (d *keystoneClientDriver) ServerScheme() string {
	if d.Client == nil {
		panic("called before Connect()")
	}
	endpointURL, err := url.Parse(d.Client.Endpoint)
	if err == nil {
		return endpointURL.Scheme
	}
	return ""
}

// SendHTTPRequest implements the client.AuthDriver interface.
func (d *keystoneClientDriver) SendHTTPRequest(req *http.Request) (*http.Response, error) {
	opts := gophercloud.RequestOpts{
		RawBody: req.Body,
		OkCodes: []int{
			http.StatusOK,
			http.StatusCreated,
			http.StatusAccepted,
			http.StatusNonAuthoritativeInfo,
			http.StatusNoContent,
		},
	}
	if len(req.Header) > 0 {
		opts.MoreHeaders = make(map[string]string, len(req.Header))
		for k, v := range req.Header {
			opts.MoreHeaders[k] = v[0]
		}
	}

	pathComponents := strings.Split(strings.Trim(req.URL.Path, "/"), "/")
	return d.Client.Request(req.Context(), req.Method, d.Client.ServiceURL(pathComponents...), &opts)
}

// CredentialsForRegistryAPI implements the client.AuthDriver interface.
func (d *keystoneClientDriver) CredentialsForRegistryAPI() (userName, password string) {
	return d.RegistryUserName, d.RegistryPassword
}
