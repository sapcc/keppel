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

package mock

import (
	"net/http"

	policy "github.com/databus23/goslo.policy"

	"github.com/sapcc/go-bits/gopherpolicy"
)

// Validator implements the gopherpolicy.Validator and gopherpolicy.Enforcer
// interfaces.
//
// During validation, the X-Auth-Token header on the request is not inspected
// at all. Instead, auth success is always assumed and a token is built from
// the Auth parameters provided during New(), using the mock itself as Enforcer.
//
// During enforcement, all accesses are allowed by default. More restrictive
// policies can be configured with Forbid() and Allow().
type Validator[E gopherpolicy.Enforcer] struct {
	Enforcer E
	Auth     map[string]string
}

// NewValidator initializes a new Validator. The provided auth variables will
// be mirrored into all gopherpolicy.Token instances returned by this Validator.
func NewValidator[E gopherpolicy.Enforcer](enforcer E, auth map[string]string) *Validator[E] {
	return &Validator[E]{enforcer, auth}
}

// CheckToken implements the gopherpolicy.Validator interface.
func (v *Validator[E]) CheckToken(r *http.Request) *gopherpolicy.Token {
	return &gopherpolicy.Token{
		Enforcer: v.Enforcer,
		Context: policy.Context{
			Auth:    v.Auth,
			Request: map[string]string{},
		},
	}
}
