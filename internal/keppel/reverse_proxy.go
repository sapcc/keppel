/******************************************************************************
*
*  Copyright 2020 SAP SE
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

package keppel

import (
	"io"
	"net/http"
	"net/url"

	"github.com/sapcc/go-bits/logg"
)

//When reverse-proxying, these headers from the client request will be
//forwarded. All other client headers will be discarded.
var reverseProxyHeaders = []string{
	"Accept",
	"Authorization",
}

//ReverseProxyAnycastRequestToPeer takes a http.Request for the anycast API and
//reverse-proxies it to a different keppel-api in this Keppel's peer group.
//After receiving the response, the caller usually continues with
//ForwardReverseProxyResponseToClient, but these are two distinct steps
//because the caller may want to check rate limits inbetween them.
//
//If ForwardReverseProxyResponseToClient is not called, the caller is
//responsible for closing the response body.
func (cfg Configuration) ReverseProxyAnycastRequestToPeer(r *http.Request, peerHostName string) (*http.Response, error) {
	//build request URL
	reqURL := url.URL{
		Scheme: "https",
		Host:   peerHostName,
		Path:   r.URL.Path,
	}

	//make the forwarding visible in the other Keppel's log file
	query := r.URL.Query()
	query.Set("forwarded-by", cfg.APIPublicURL.Hostname())
	reqURL.RawQuery = query.Encode()

	//send proxy request
	req, err := http.NewRequest(r.Method, reqURL.String(), nil)
	if err != nil {
		return nil, err
	}
	for _, headerName := range reverseProxyHeaders {
		req.Header[headerName] = r.Header[headerName]
	}
	req.Header.Set("X-Keppel-Forwarded-By", cfg.APIPublicURL.Hostname())
	return http.DefaultClient.Do(req) //TODO: do not resolve 3xx responses
}

//ForwardReverseProxyResponseToClient is used when
//ReverseProxyAnycastRequestToPeer was successful and the response received
//from the peer shall be forwarded to the original requester.
func (cfg Configuration) ForwardReverseProxyResponseToClient(w http.ResponseWriter, resp *http.Response) {
	for k, v := range resp.Header {
		w.Header()[k] = v
	}
	w.WriteHeader(resp.StatusCode)

	_, err := io.Copy(w, resp.Body)
	if err == nil {
		err = resp.Body.Close()
	} else {
		resp.Body.Close()
	}
	if err != nil {
		logg.Error("while forwarding reverse-proxy response to caller: " + err.Error())
	}
}
