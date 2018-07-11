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
	"net/http"
	"strconv"
	"strings"
)

//iteratorInterface allows iteratorBase to access public attributes of
//ContainerIterator/ObjectIterator.
type iteratorInterface interface {
	getAccount() *Account
	getContainerName() string
	getDelimiter() string
	getPrefix() string
	getOptions() *RequestOptions
	//putHeader initializes the AccountHeaders/ContainerHeaders field of the
	//Account/Container using the response headers from the GET request.
	putHeader(http.Header) error
}

func (i ContainerIterator) getAccount() *Account        { return i.Account }
func (i ContainerIterator) getContainerName() string    { return "" }
func (i ContainerIterator) getDelimiter() string        { return "" }
func (i ContainerIterator) getPrefix() string           { return i.Prefix }
func (i ContainerIterator) getOptions() *RequestOptions { return i.Options }

func (i ContainerIterator) putHeader(hdr http.Header) error {
	headers := AccountHeaders{headersFromHTTP(hdr)}
	if err := headers.Validate(); err != nil {
		return err
	}
	i.Account.headers = &headers
	return nil
}

func (i ObjectIterator) getAccount() *Account        { return i.Container.Account() }
func (i ObjectIterator) getContainerName() string    { return i.Container.Name() }
func (i ObjectIterator) getDelimiter() string        { return i.Delimiter }
func (i ObjectIterator) getPrefix() string           { return i.Prefix }
func (i ObjectIterator) getOptions() *RequestOptions { return i.Options }

func (i ObjectIterator) putHeader(hdr http.Header) error {
	headers := ContainerHeaders{headersFromHTTP(hdr)}
	if err := headers.Validate(); err != nil {
		return err
	}
	i.Container.headers = &headers
	return nil
}

//iteratorBase provides shared behavior for ContainerIterator and ObjectIterator.
type iteratorBase struct {
	i      iteratorInterface
	marker string
	eof    bool
}

func (b *iteratorBase) request(limit int, detailed bool) Request {
	r := Request{
		Method:        "GET",
		ContainerName: b.i.getContainerName(),
		Options:       cloneRequestOptions(b.i.getOptions(), nil),
	}

	if delimiter := b.i.getDelimiter(); delimiter != "" {
		r.Options.Values.Set("delimiter", delimiter)
	}
	if prefix := b.i.getPrefix(); prefix != "" {
		r.Options.Values.Set("prefix", prefix)
	}

	if b.marker == "" {
		r.Options.Values.Del("marker")
	} else {
		r.Options.Values.Set("marker", b.marker)
	}

	if limit < 0 {
		r.Options.Values.Del("limit")
	} else {
		r.Options.Values.Set("limit", strconv.FormatUint(uint64(limit), 10))
	}

	if detailed {
		r.Options.Headers.Set("Accept", "application/json")
		r.Options.Values.Set("format", "json")
		r.ExpectStatusCodes = []int{200}
	} else {
		r.Options.Headers.Set("Accept", "text/plain")
		r.Options.Values.Set("format", "plain")
		r.ExpectStatusCodes = []int{200, 204}
	}

	return r
}

func (b *iteratorBase) nextPage(limit int) ([]string, error) {
	if b.eof {
		return nil, nil
	}
	resp, err := b.request(limit, false).Do(b.i.getAccount().backend)
	if err != nil {
		return nil, err
	}

	buf, err := collectResponseBody(resp)
	if err != nil {
		return nil, err
	}
	bufStr := strings.TrimSuffix(string(buf), "\n")
	var result []string
	if bufStr != "" {
		result = strings.Split(bufStr, "\n")
	}

	if len(result) == 0 {
		b.eof = true
		b.marker = ""
	} else {
		b.eof = false
		b.marker = result[len(result)-1]
	}
	return result, b.i.putHeader(resp.Header)
}

func (b *iteratorBase) nextPageDetailed(limit int, data interface{}) error {
	if b.eof {
		return nil
	}
	resp, err := b.request(limit, true).Do(b.i.getAccount().backend)
	if err != nil {
		return err
	}

	err = json.NewDecoder(resp.Body).Decode(&data)
	closeErr := resp.Body.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = b.i.putHeader(resp.Header)
	}
	return err
}

func (b *iteratorBase) setMarker(marker string) {
	b.marker = marker
	b.eof = marker == ""
}
