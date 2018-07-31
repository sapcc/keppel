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

package api

import (
	"net/http"
	"strings"

	"github.com/sapcc/go-bits/respondwith"
	"github.com/sapcc/keppel/pkg/auth"
	"github.com/sapcc/keppel/pkg/database"
	"github.com/sapcc/keppel/pkg/keppel"
)

func (api *KeppelV1) handleGetAuth(w http.ResponseWriter, r *http.Request) {
	//parse request
	req, err := auth.ParseRequest(
		r.Header.Get("Authorization"),
		r.URL.RawQuery,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_ = req

	//find account if scope requested
	var account *database.Account
	if req.Scope != nil && req.Scope.ResourceType == "repository" {
		accountName := strings.SplitN(req.Scope.ResourceName, "/", 2)[0]
		account, err = keppel.State.DB.FindAccount(accountName)
		if respondwith.ErrorText(w, err) {
			return
		}
		//do not check account == nil here yet to not leak account existence to
		//unauthorized users
	}

	//check user access
	access, err := keppel.State.ServiceUser.CheckUserAccess(
		req.UserName, req.Password, account)
	if err != nil {
		http.Error(w, err.Error(), 401)
		return
	}

	//TODO match Keystone roles with requested scope and actions
	//TODO generate/serialize JWT
	respondwith.JSON(w, http.StatusOK, access)
}
