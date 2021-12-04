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
	"crypto"
	"fmt"
	"strings"

	"github.com/sapcc/keppel/internal/keppel"
)

//Audience is an audience for which we can issue tokens.
//
//NOTE: All members need to be public since audiences are JSON-serialized in
//test.Setup().getToken().
type Audience struct {
	IsAnycast bool
	//When using a domain-remapped API, contains the account name specified in the domain name.
	//Otherwise, contains the empty string.
	AccountName string
}

//IdentifyAudience returns the Audience corresponding to the given domain name.
//The hostname argument is either directly taken from a request URL (for
//regular API requests), from the "service" value of auth requests, or from the
//"audience" field in tokens.
func IdentifyAudience(hostname string, cfg keppel.Configuration) Audience {
	//option 1: the hostname is directly known to us
	if hostname != "" {
		switch hostname {
		case cfg.APIPublicHostname:
			return Audience{IsAnycast: false, AccountName: ""}
		case cfg.AnycastAPIPublicHostname:
			return Audience{IsAnycast: true, AccountName: ""}
		}
	}

	//option 2: the hostname is for a domain-remapped API
	hostnameParts := strings.SplitN(hostname, ".", 2)
	if len(hostnameParts) == 2 && hostnameParts[0] != "" && hostnameParts[1] != "" {
		//head must look like an account name...
		if keppel.IsAccountName(hostnameParts[0]) {
			//...and tail must be one of the well-known hostnames
			switch hostnameParts[1] {
			case cfg.APIPublicHostname:
				return Audience{IsAnycast: false, AccountName: hostnameParts[0]}
			case cfg.AnycastAPIPublicHostname:
				return Audience{IsAnycast: true, AccountName: hostnameParts[0]}
			}
		}
	}

	//when we don't know what's going on with the hostname at all, we fallback to the default
	return Audience{IsAnycast: false, AccountName: ""}
}

//Hostname returns the hostname that is used as the "audience" value in tokens
//and as the "service" value in auth challenges. This is the inverse operation
//of IdentifyAudience in the following sense:
//
//    audience == IdentifyAudience(audience.Hostname(cfg), cfg)
//
func (a Audience) Hostname(cfg keppel.Configuration) string {
	result := cfg.APIPublicHostname
	if a.IsAnycast {
		result = cfg.AnycastAPIPublicHostname
	}
	if a.AccountName != "" {
		result = fmt.Sprintf("%s.%s", a.AccountName, result)
	}
	return result
}

//PeerHostname takes the KEPPEL_API_PUBLIC_FQDN of a peer, and adds
//domain-remapping to it if necessary. This is used when reverse-proxying
//anycast requests to a peer, to ensure that domain-remapped requests stay
//domain-remapped.
func (a Audience) MapPeerHostname(peerHostname string) string {
	result := peerHostname
	if a.AccountName != "" {
		result = fmt.Sprintf("%s.%s", a.AccountName, result)
	}
	return result
}

//IssuerKeys returns the issuer keys that are used to sign tokens for this
//service. Index [0] contains the key that shall be used for new tokens, but
//all keys are acceptable in existing tokens (to support seamless key
//rotation).
func (a Audience) IssuerKeys(cfg keppel.Configuration) []crypto.PrivateKey {
	if a.IsAnycast {
		return cfg.AnycastJWTIssuerKeys
	}
	return cfg.JWTIssuerKeys
}
