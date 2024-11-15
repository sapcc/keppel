/*******************************************************************************
*
* Copyright 2024 SAP SE
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

// Package gophercloudext contains convenience functions for use with [Gophercloud].
// It is specifically intended as a lightweight replacement for [gophercloud/utils] with fewer dependencies.
//
// [Gophercloud]: https://github.com/gophercloud/gophercloud
// [gophercloud/utils]: https://github.com/gophercloud/utils
package gophercloudext

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"

	"github.com/sapcc/go-bits/osext"
)

// ClientOpts contains configuration for NewProviderClient().
type ClientOpts struct {
	// EnvPrefix allows a custom environment variable prefix to be used.
	// If not set, "OS_" is used.
	EnvPrefix string

	// HTTPClient is the ProviderClient's internal HTTP client.
	// If not set, a fresh http.Client using http.DefaultTransport will be used.
	//
	// This is a weird behavior, but we cannot do better because
	// gophercloud.ProviderClient insists on taking ownership of whatever is
	// given here, so we cannot just give http.DefaultClient here.
	HTTPClient *http.Client

	// CustomizeAuthOptions is a callback that can be used to modify the
	// constructed AuthOptions before they are passed to the ProviderClient.
	//
	// This is used in rare special cases, e.g. when an application needs to
	// spawn clients with different token scopes for specific operations.
	CustomizeAuthOptions func(*gophercloud.AuthOptions)
}

// NewProviderClient authenticates with OpenStack using the credentials found
// in the usual OS_* environment variables.
//
// Ref: https://docs.openstack.org/python-openstackclient/latest/cli/man/openstack.html
//
// This function has the same purpose as AuthenticatedClient from package
// github.com/gophercloud/utils/openstack/clientconfig, except for some
// differences that make it specifically suited for long-running server
// applications and remove functionality only needed for interactive use:
//
//   - It always sets AllowReauth on the ProviderClient.
//   - It does not support authenticating with a pre-existing Keystone token.
//   - It does not support reading clouds.yaml files.
//   - It does not support the old Keystone v2 authentication (only v3).
//
// Also, to simplify things, some legacy or fallback environment variables are
// not supported:
//
//   - OS_TENANT_ID (give OS_PROJECT_ID instead)
//   - OS_TENANT_NAME (give OS_PROJECT_NAME instead)
//   - OS_DEFAULT_DOMAIN_ID (give OS_PROJECT_DOMAIN_ID and OS_USER_DOMAIN_ID instead)
//   - OS_DEFAULT_DOMAIN_NAME (give OS_PROJECT_DOMAIN_NAME and OS_USER_DOMAIN_NAME instead)
//   - OS_APPLICATION_CREDENTIAL_NAME (give OS_APPLICATION_CREDENTIAL_ID instead)
func NewProviderClient(ctx context.Context, optsPtr *ClientOpts) (*gophercloud.ProviderClient, gophercloud.EndpointOpts, error) {
	// apply defaults to `opts`
	var opts ClientOpts
	if optsPtr != nil {
		opts = *optsPtr
	}
	if opts.EnvPrefix == "" {
		opts.EnvPrefix = "OS_"
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{}
	}

	// expect an auth URL for v3
	authURL, err := osext.NeedGetenv(opts.EnvPrefix + "AUTH_URL")
	if err != nil {
		return nil, gophercloud.EndpointOpts{}, err
	}
	if !strings.Contains(authURL, "/v3") {
		return nil, gophercloud.EndpointOpts{}, fmt.Errorf(
			"expected %sAUTH_URL to refer to Keystone v3, but got %s", opts.EnvPrefix, authURL,
		)
	}

	// most other consistency checks are delegated to gophercloud.AuthOptions,
	// so we just build an AuthOptions without checking a lot of stuff
	scope := gophercloud.AuthScope{
		ProjectID:   os.Getenv(opts.EnvPrefix + "PROJECT_ID"),
		ProjectName: os.Getenv(opts.EnvPrefix + "PROJECT_NAME"),
	}
	if scope.ProjectID == "" && scope.ProjectName == "" {
		// not project scope, so might be domain scope
		scope.DomainID = os.Getenv(opts.EnvPrefix + "DOMAIN_ID")
		scope.DomainName = os.Getenv(opts.EnvPrefix + "DOMAIN_NAME")
		if scope.DomainID == "" && scope.DomainName == "" {
			// not domain scope either, so might be system scope
			scope.System = os.Getenv(opts.EnvPrefix+"SYSTEM_SCOPE") != ""
		}
	} else {
		// definitely project scope
		scope.DomainID = os.Getenv(opts.EnvPrefix + "PROJECT_DOMAIN_ID")
		scope.DomainName = os.Getenv(opts.EnvPrefix + "PROJECT_DOMAIN_NAME")
	}
	ao := gophercloud.AuthOptions{
		IdentityEndpoint:            authURL,
		Username:                    os.Getenv(opts.EnvPrefix + "USERNAME"),
		UserID:                      os.Getenv(opts.EnvPrefix + "USER_ID"),
		DomainName:                  os.Getenv(opts.EnvPrefix + "USER_DOMAIN_NAME"),
		DomainID:                    os.Getenv(opts.EnvPrefix + "USER_DOMAIN_ID"),
		Password:                    os.Getenv(opts.EnvPrefix + "PASSWORD"),
		AllowReauth:                 true,
		Scope:                       &scope,
		ApplicationCredentialID:     os.Getenv(opts.EnvPrefix + "APPLICATION_CREDENTIAL_ID"),
		ApplicationCredentialSecret: os.Getenv(opts.EnvPrefix + "APPLICATION_CREDENTIAL_SECRET"),
	}
	if opts.CustomizeAuthOptions != nil {
		opts.CustomizeAuthOptions(&ao)
	}

	provider, err := openstack.NewClient(ao.IdentityEndpoint)
	if err == nil {
		provider.HTTPClient = *opts.HTTPClient
		err = openstack.Authenticate(ctx, provider, ao)
	}
	if err != nil {
		return nil, gophercloud.EndpointOpts{}, fmt.Errorf(
			"cannot initialize OpenStack client from %s* variables: %w", opts.EnvPrefix, err)
	}

	eo := gophercloud.EndpointOpts{
		Availability: gophercloud.Availability(os.Getenv(opts.EnvPrefix + "INTERFACE")),
		Region:       os.Getenv(opts.EnvPrefix + "REGION_NAME"),
	}
	return provider, eo, nil
}
