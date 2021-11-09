/*******************************************************************************
*
* Copyright 2021 SAP SE
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

	"github.com/docker/libtrust"
	"github.com/sapcc/keppel/internal/keppel"
)

//Service enumerates the different audiences for which we can issue tokens.
type Service int

const (
	//LocalService is the Service for tokens issued by this keppel-api for use
	//only with the same keppel-api.
	LocalService Service = 0
	//AnycastService is the Service for tokens issued by this keppel-api or one
	//of its peers for their shared anycast endpoint.
	AnycastService Service = 1
)

//Hostname returns the hostname that is used as the "audience" value in tokens
//and as the "service" value in auth challenges.
func (s Service) Hostname(cfg keppel.Configuration) string {
	switch s {
	case LocalService:
		return cfg.APIPublicURL.Hostname()
	case AnycastService:
		return cfg.AnycastAPIPublicURL.Hostname()
	default:
		panic(fmt.Sprintf("unknown auth service code: %d", s))
	}
}

//IssuerKey returns the issuer key that is used to sign tokens for this
//service.
func (s Service) IssuerKey(cfg keppel.Configuration) libtrust.PrivateKey {
	switch s {
	case LocalService:
		return cfg.JWTIssuerKey
	case AnycastService:
		return *cfg.AnycastJWTIssuerKey
	default:
		panic(fmt.Sprintf("unknown auth service code: %d", s))
	}
}
