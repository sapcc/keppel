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

package gopherpolicy

import (
	"encoding/json"
	"fmt"

	policy "github.com/databus23/goslo.policy"
)

// SerializeCompactContextToJSON takes a policy.Context constructed by this package,
// and compresses its Auth and Roles fields into an extremely compact form.
// This format is intended for serialization in places where every last byte
// counts, e.g. in JWT payloads.
//
// Its inverse is DeserializeCompactContextFromJSON.
func SerializeCompactContextToJSON(c policy.Context) ([]byte, error) {
	a := c.Auth
	s := serializedContext{
		Version: 1,
		User:    []string{a["user_id"], a["user_name"]},
		Roles:   c.Roles,
	}

	// serialize scope (if any)
	var scopeDomainID string
	if projectID := a["project_id"]; projectID != "" {
		s.Project = []string{projectID, a["project_name"]}
		scopeDomainID = a["project_domain_id"]
		s.Domain = []string{scopeDomainID, a["project_domain_name"]}
	} else if domainID := a["domain_id"]; domainID != "" {
		s.Domain = []string{domainID, a["domain_name"]}
		scopeDomainID = domainID
	}

	// serialize additional user information
	if userDomainID := a["user_domain_id"]; userDomainID != scopeDomainID {
		s.UserDomain = []string{userDomainID, a["user_domain_name"]}
	}
	if appCredID := a["application_credential_id"]; appCredID != "" {
		s.ApplicationCredential = []string{appCredID, a["application_credential_name"]}
	}

	return json.Marshal(s)
}

type serializedContext struct {
	// Future-proofing: If we need to change this format in the future,
	// we can increase this to enable backwards-compatibility if necessary.
	Version uint8 `json:"v"`

	// If any of these fields is present, it must contain exactly two elements
	// ([0] = ID, [1] = Name). We are encoding these pairs as lists because
	// ["foo","bar"] is shorter than {"i":"foo","n":"bar"}.
	Project               []string `json:"p,omitempty"` // project scope only
	Domain                []string `json:"d,omitempty"` // refers either to the domain scope, or to the domain of the project scope
	User                  []string `json:"u,omitempty"`
	UserDomain            []string `json:"ud,omitempty"` // omitted if "d" is present and contains the same value
	ApplicationCredential []string `json:"ac,omitempty"` // only if token was spawned from an application credential (SAPCC extension)

	Roles []string `json:"r"`
}

// DeserializeCompactContextFromJSON is the inverse of SerializeCompactContextToJSON.
func DeserializeCompactContextFromJSON(buf []byte) (policy.Context, error) {
	var s serializedContext
	err := json.Unmarshal(buf, &s)
	if err != nil {
		return policy.Context{}, err
	}
	if s.Version != 1 {
		return policy.Context{}, fmt.Errorf("unknown format version: %d", s.Version)
	}

	// unpack user information
	auth := make(map[string]string)
	auth["user_id"], auth["user_name"], err = unpackIDAndNamePair("u", s.User)
	if err != nil {
		return policy.Context{}, err
	}
	auth["user_domain_id"], auth["user_domain_name"], err = unpackIDAndNamePair("ud", s.UserDomain)
	if err != nil {
		return policy.Context{}, err
	}
	auth["application_credential_id"], auth["application_credential_name"], err = unpackIDAndNamePair("ud", s.ApplicationCredential)
	if err != nil {
		return policy.Context{}, err
	}

	// unpack scope, if any
	hasProjectScope := len(s.Project) > 0
	if hasProjectScope {
		projectID, projectName, err := unpackIDAndNamePair("p", s.Project)
		if err != nil {
			return policy.Context{}, err
		}
		auth["project_id"] = projectID
		auth["project_name"] = projectName
		auth["tenant_id"] = projectID
		auth["tenant_name"] = projectName
	}
	if len(s.Domain) > 0 {
		domainID, domainName, err := unpackIDAndNamePair("d", s.Domain)
		if err != nil {
			return policy.Context{}, err
		}

		if hasProjectScope {
			auth["project_domain_id"] = domainID
			auth["project_domain_name"] = domainName
			auth["tenant_domain_id"] = domainID
			auth["tenant_domain_name"] = domainName
		} else {
			auth["domain_id"] = domainID
			auth["domain_name"] = domainName
		}

		if auth["user_domain_id"] == "" {
			auth["user_domain_id"] = domainID
			auth["user_domain_name"] = domainName
		}
	}

	// remove empty values that we unpacked from optional fields
	for key, value := range auth {
		if value == "" {
			delete(auth, key)
		}
	}

	return policy.Context{
		Auth:  auth,
		Roles: s.Roles,
	}, nil
}

func unpackIDAndNamePair(key string, pair []string) (id, name string, err error) {
	switch len(pair) {
	case 0:
		return "", "", nil
	case 2:
		return pair[0], pair[1], nil
	default:
		return "", "", fmt.Errorf("invalid payload in field %q: expected 0 or 2 fields, but got %#v", key, pair)
	}
}
