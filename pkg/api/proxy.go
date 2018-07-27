/******************************************************************************
*
*  Copyright 2018 Stefan Majewsky <majewsky@gmx.net>
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

package api

import (
	"io"
	"net"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/go-bits/respondwith"
)

func (api *KeppelV1) handleProxyToAccount(w http.ResponseWriter, r *http.Request) {
	accountName := mux.Vars(r)["account"]
	account, err := api.db.FindAccount(accountName)
	if respondwith.ErrorText(w, err) {
		return
	}
	if account == nil {
		//TODO respond in the same way as the registry would on Unauthorized, to
		//not leak information about which accounts exist to unauthorized users
		//
		//We might have to do the full auth game right here already before even
		//proxying to keppel-registry, but that would require recognizing all API
		//endpoints.
		http.Error(w, "not found", 404)
		return
	}

	proxyRequest := *r
	proxyRequest.URL.Scheme = "http"
	proxyRequest.URL.Host = api.orch.GetHostPortForAccount(*account)
	//remove account name from URL path
	proxyRequest.URL.Path = "/v2/" + strings.TrimPrefix(
		proxyRequest.URL.Path, "/v2/"+account.Name+"/")

	proxyRequest.Close = false
	proxyRequest.RequestURI = ""
	if proxyRequest.RemoteAddr != "" && proxyRequest.Header.Get("X-Forwarded-For") == "" {
		host, _, _ := net.SplitHostPort(proxyRequest.RemoteAddr)
		proxyRequest.Header.Set("X-Forwarded-For", host)
	}

	resp, err := http.DefaultClient.Do(&proxyRequest)
	if respondwith.ErrorText(w, err) {
		return
	}
	defer resp.Body.Close()

	for k, v := range resp.Header {
		w.Header()[k] = v
	}
	w.WriteHeader(resp.StatusCode)
	_, err = io.Copy(w, resp.Body)
	if err != nil {
		logg.Error("error copying proxy response: " + err.Error())
	}
}
