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

package keppelv1

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
)

//SubleaseToken is the internal structure of a sublease token. Only the secret
//is passed on to the federation driver. The other attributes are only
//informational. GUIs/CLIs can display these data to the user for confirmation
//when the token is entered.
type SubleaseToken struct {
	AccountName     string `json:"account"`
	PrimaryHostname string `json:"primary"`
	Secret          string `json:"secret"`
}

//Serialize returns the Base64-encoded JSON of this token. This is the format
//that gets passed to the user.
func (t SubleaseToken) Serialize() string {
	buf, _ := json.Marshal(t)
	return base64.StdEncoding.EncodeToString(buf)
}

//SubleaseTokenFromRequest parses the request's X-Keppel-Sublease-Token header.
func SubleaseTokenFromRequest(r *http.Request) (SubleaseToken, error) {
	in := r.Header.Get("X-Keppel-Sublease-Token")
	if in == "" {
		return SubleaseToken{}, nil //empty sublease token is acceptable for federation drivers that don't need one
	}

	buf, err := base64.StdEncoding.DecodeString(in)
	if err != nil {
		return SubleaseToken{}, fmt.Errorf("malformed X-Keppel-Sublease-Token header: %s", err.Error())
	}

	var t SubleaseToken
	err = json.Unmarshal(buf, &t)
	if err != nil {
		return SubleaseToken{}, fmt.Errorf("malformed X-Keppel-Sublease-Token header: %s", err.Error())
	}
	return t, nil
}
