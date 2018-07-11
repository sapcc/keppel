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

package schwift

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
)

//Account represents a Swift account. Instances are usually obtained by
//connecting to a backend (see package-level documentation), or by traversing
//upwards from a container with Container.Account().
type Account struct {
	backend Backend
	//URL parts
	baseURL string
	name    string
	//cache
	headers *AccountHeaders
	caps    *Capabilities
}

//IsEqualTo returns true if both Account instances refer to the same account.
func (a *Account) IsEqualTo(other *Account) bool {
	return other.baseURL == a.baseURL && other.name == a.name
}

var endpointURLRegexp = regexp.MustCompile(`^(.*/)v1/(.*)/$`)

//InitializeAccount takes something that implements the Backend interface, and
//returns the Account instance corresponding to the account/project that this
//backend is connected to.
func InitializeAccount(backend Backend) (*Account, error) {
	match := endpointURLRegexp.FindStringSubmatch(backend.EndpointURL())
	if match == nil {
		return nil, fmt.Errorf(`schwift.AccountFromClient(): invalid Swift endpoint URL: cannot find "/v1/" in %q`, backend.EndpointURL())
	}
	return &Account{
		backend: backend,
		baseURL: match[1],
		name:    match[2],
	}, nil
}

//SwitchAccount returns a handle to a different account on the same server. Note
//that you need reseller permissions to access accounts other than that where
//you originally authenticated. This method does not check whether the account
//actually exists.
//
//The account name is usually the Keystone project ID with an additional "AUTH_"
//prefix.
func (a *Account) SwitchAccount(accountName string) *Account {
	newEndpointURL := a.baseURL + "v1/" + accountName + "/"
	return &Account{
		backend: a.backend.Clone(newEndpointURL),
		baseURL: a.baseURL,
		name:    accountName,
	}
}

//Name returns the name of the account (usually the prefix "AUTH_" followed by
//the Keystone project ID).
func (a *Account) Name() string {
	return a.name
}

//Backend returns the backend which is used to make requests against this
//account.
func (a *Account) Backend() Backend {
	return a.backend
}

//Headers returns the AccountHeaders for this account. If the AccountHeaders
//has not been cached yet, a HEAD request is issued on the account.
//
//This operation fails with http.StatusNotFound if the account does not exist.
func (a *Account) Headers() (AccountHeaders, error) {
	if a.headers != nil {
		return *a.headers, nil
	}

	resp, err := Request{
		Method:            "HEAD",
		ExpectStatusCodes: []int{204},
	}.Do(a.backend)
	if err != nil {
		return AccountHeaders{}, err
	}

	headers := AccountHeaders{headersFromHTTP(resp.Header)}
	err = headers.Validate()
	if err != nil {
		return headers, err
	}
	a.headers = &headers
	return *a.headers, nil
}

//Invalidate clears the internal cache of this Account instance. The next call
//to Headers() on this instance will issue a HEAD request on the account.
func (a *Account) Invalidate() {
	a.headers = nil
}

//Update updates the account using a POST request. The headers in the headers
//attribute take precedence over those in opts.Headers.
//
//A successful POST request implies Invalidate() since it may change metadata.
func (a *Account) Update(headers AccountHeaders, opts *RequestOptions) error {
	_, err := Request{
		Method:            "POST",
		Options:           cloneRequestOptions(opts, headers.Headers),
		ExpectStatusCodes: []int{204},
	}.Do(a.backend)
	if err == nil {
		a.Invalidate()
	}
	return err
}

//Create creates the account using a PUT request. This operation is only
//available to reseller admins, not to regular users.
//
//A successful PUT request implies Invalidate() since it may change metadata.
func (a *Account) Create(opts *RequestOptions) error {
	_, err := Request{
		Method:            "PUT",
		Options:           opts,
		ExpectStatusCodes: []int{201, 202},
		DrainResponseBody: true,
	}.Do(a.backend)
	if err == nil {
		a.Invalidate()
	}
	return err
}

//Containers returns a ContainerIterator that lists the containers in this
//account. The most common use case is:
//
//	containers, err := account.Containers().Collect()
//
//You can extend this by configuring the iterator before collecting the results:
//
//	iter := account.Containers()
//	iter.Prefix = "test-"
//	containers, err := iter.Collect()
//
//Or you can use a different iteration method:
//
//	err := account.Containers().ForeachDetailed(func (ci ContainerInfo) error {
//		log.Printf("container %s contains %d objects!\n",
//			ci.Container.Name(), ci.ObjectCount)
//	})
//
func (a *Account) Containers() *ContainerIterator {
	return &ContainerIterator{Account: a}
}

//Capabilities queries the GET /info endpoint of the Swift server providing
//this account. Capabilities are cached, so the GET request will only be sent
//once during the first call to this method.
func (a *Account) Capabilities() (Capabilities, error) {
	if a.caps != nil {
		return *a.caps, nil
	}

	buf, err := a.RawCapabilities()
	if err != nil {
		return Capabilities{}, err
	}

	var caps Capabilities
	err = json.Unmarshal(buf, &caps)
	if err != nil {
		return caps, err
	}

	a.caps = &caps
	return caps, nil
}

//RawCapabilities queries the GET /info endpoint of the Swift server providing
//this account, and returns the response body. Unlike Account.Capabilities,
//this method does not employ any caching.
func (a *Account) RawCapabilities() ([]byte, error) {
	//This method is the only one in Schwift that bypasses struct Request since
	//the request URL is not below the endpoint URL.
	req, err := http.NewRequest("GET", a.baseURL+"info", nil)
	if err != nil {
		return nil, err
	}
	resp, err := a.backend.Do(req)
	if err != nil {
		return nil, err
	}
	return collectResponseBody(resp)
}
