/*******************************************************************************
*
* Copyright 2017-2018 SAP SE
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
	"net/http"

	policy "github.com/databus23/goslo.policy"
	"github.com/gophercloud/gophercloud"
)

//Enforcer contains the Enforce method that struct Token requires to check
//access permissions. This interface is satisfied by struct Enforcer from
//goslo.policy.
type Enforcer interface {
	Enforce(rule string, c policy.Context) bool
}

//Token represents a validated Keystone v3 token. It is returned from
//Validator.CheckToken().
type Token struct {
	//The enforcer that checks access permissions for this client token. Usually
	//an instance of struct Enforcer from goslo.policy. Usually inherited from
	//struct TokenValidator.
	Enforcer Enforcer
	//When AuthN succeeds, contains information about the client token which can
	//be used to check access permissions.
	Context policy.Context
	//When AuthN succeeds, contains a fully-initialized ProviderClient with which
	//this process can use the OpenStack API on behalf of the authenticated user.
	ProviderClient *gophercloud.ProviderClient
	//When AuthN fails, contains the deferred AuthN error.
	Err error
}

//Require checks if the given token has the given permission according to the
//policy.json that is in effect. If not, an error response is written and false
//is returned.
func (t *Token) Require(w http.ResponseWriter, rule string) bool {
	if t.Err != nil {
		http.Error(w, "Unauthorized", 401)
		return false
	}

	if !t.Enforcer.Enforce(rule, t.Context) {
		http.Error(w, "Forbidden", 403)
		return false
	}
	return true
}

//Check is like Require, but does not write error responses.
func (t *Token) Check(rule string) bool {
	return t.Err == nil && t.Enforcer.Enforce(rule, t.Context)
}

//UserUUID returns the UUID of the user for whom this token was issued, or ""
//if the token was invalid.
func (t *Token) UserUUID() string {
	return t.Context.Auth["user_id"]
}

//UserName returns the name of the user for whom this token was issued, or ""
//if the token was invalid.
func (t *Token) UserName() string {
	return t.Context.Auth["user_name"]
}

func extractErrorMessage(err error) string {
	switch e := err.(type) {
	case gophercloud.ErrUnexpectedResponseCode:
		return e.Error()
	case gophercloud.ErrDefault401:
		return e.ErrUnexpectedResponseCode.Error()
	case gophercloud.ErrDefault403:
		return e.ErrUnexpectedResponseCode.Error()
	case gophercloud.ErrDefault404:
		return e.ErrUnexpectedResponseCode.Error()
	case gophercloud.ErrDefault405:
		return e.ErrUnexpectedResponseCode.Error()
	case gophercloud.ErrDefault408:
		return e.ErrUnexpectedResponseCode.Error()
	case gophercloud.ErrDefault429:
		return e.ErrUnexpectedResponseCode.Error()
	case gophercloud.ErrDefault500:
		return e.ErrUnexpectedResponseCode.Error()
	case gophercloud.ErrDefault503:
		return e.ErrUnexpectedResponseCode.Error()
	default:
		return err.Error()
	}
}
