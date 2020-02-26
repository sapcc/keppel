/*******************************************************************************
*
* Copyright 2018 SAP SE
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

package auth

import (
	"fmt"
	"net/http"
	"net/url"

	"github.com/sapcc/keppel/internal/keppel"
)

//Challenge describes an auth challenge that is posed in a Www-Authenticate
//response header.
type Challenge struct {
	Scope *Scope //optional
	Error string //optional

	//optional: if set, asks for the client to use the /keppel/v1/auth port at
	//this host (this allows the API to take into account which hostname and
	//scheme was used by the client to connect via a reverse proxy)
	OverrideAPIHost   string
	OverrideAPIScheme string
}

//WriteTo adds the corresponding Www-Authenticate header to a response.
func (c Challenge) WriteTo(h http.Header, cfg keppel.Configuration) {
	apiURL := cfg.APIPublicURL.String()
	if c.OverrideAPIHost != "" && c.OverrideAPIScheme != "" {
		apiURL = (&url.URL{Scheme: c.OverrideAPIScheme, Host: c.OverrideAPIHost}).String()
	}

	fields := fmt.Sprintf(`realm="%s",service="%s"`,
		apiURL+"/keppel/v1/auth",
		cfg.APIPublicHostname(),
	)

	if c.Scope != nil {
		fields += fmt.Sprintf(`,scope="%s"`, c.Scope.String())
	}
	if c.Error != "" {
		fields += fmt.Sprintf(`,error="%s"`, c.Error)
	}

	h.Set("Www-Authenticate", "Bearer "+fields)
}
