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

package openstack

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/identity/v3/tokens"
	"github.com/sapcc/go-bits/gopherpolicy"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/keppel/pkg/database"
)

//This type is not exported to reduce coupling between the generic and the
//Openstack-specific parts of Keppel. Users outside this package will use the
//AccessLevel interface implemented by this type.
type token struct {
	*gopherpolicy.Token
}

//GetAccessLevelForRequest validates the token in this request's X-Auth-Token
//header. If a non-nil error is returned, the HTTP handler should generate a
//401 response.
func (su *ServiceUser) GetAccessLevelForRequest(r *http.Request) (AccessLevel, error) {
	t := su.tokenValidator.CheckToken(r)
	if t.Err != nil {
		return nil, t.Err
	}
	t.Context.Logger = logg.Debug
	//token.Context.Request = mux.Vars(r) //not used at the moment
	return token{t}, nil
}

//GetAccessLevelForUser checks whether the given user has access to the project
//corresponding to the given account (or, for account == nil, just whether the
//user exists and the correct password was given).
//The user name must be given as "username@userdomainname".
//
//Returns (access, nil) if the user exists and has access to the account.
//
//Returns (nil, nil) if the user exists, but does not have access (or no
//account was specified).
//
//Returns (nil, err) in case of authentication failure (e.g. wrong
//username/password) or other failures (e.g. Keystone API down).
func (su *ServiceUser) GetAccessLevelForUser(username, password string, account *database.Account) (AccessLevel, error) {
	usernameFields := strings.SplitN(username, "@", 2)
	if len(usernameFields) != 2 {
		return nil, errors.New(`invalid username (expected "user@domain" format)`)
	}

	authOpts := gophercloud.AuthOptions{
		IdentityEndpoint: su.identityV3.Endpoint,
		Username:         usernameFields[0],
		DomainName:       usernameFields[1],
		Password:         password,
	}
	if account != nil {
		authOpts.Scope = &gophercloud.AuthScope{ProjectID: account.ProjectUUID}
	}

	result := tokens.Create(su.identityV3, &authOpts)
	t := su.tokenValidator.TokenFromGophercloudResult(result)
	return &token{t}, t.Err
}

//CanViewAccounts implements the AccessLevel interface.
func (t token) CanViewAccounts() bool {
	return t.Token.Check("account:list")
}

//CanViewAccount implements the AccessLevel interface.
func (t token) CanViewAccount(account database.Account) bool {
	t.Token.Context.Request["account_project_id"] = account.ProjectUUID
	return t.Token.Check("account:show")
}

//CanChangeAccount implements the AccessLevel interface.
func (t token) CanChangeAccount(account database.Account) bool {
	t.Token.Context.Request["account_project_id"] = account.ProjectUUID
	return t.Token.Check("account:edit")
}
