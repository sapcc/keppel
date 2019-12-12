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

package authapi

import (
	"time"

	"github.com/sapcc/keppel/internal/auth"
	"github.com/sapcc/keppel/internal/keppel"
)

//TokenResponse is the format expected by Docker in an auth response. The Token
//field contains a Java Web Token (JWT).
type TokenResponse struct {
	Token     string `json:"token"`
	ExpiresIn uint64 `json:"expires_in"`
	IssuedAt  string `json:"issued_at"`
}

//ToResponse renders this token as a Java Web Token and returns a JSON-serializable
//struct in the format expected by Docker in an auth response.
func makeTokenResponse(t auth.Token, cfg keppel.Configuration) (*TokenResponse, error) {
	issuedToken, err := t.Issue(cfg)
	if err != nil {
		return nil, err
	}
	return &TokenResponse{
		Token:     issuedToken.SignedToken,
		ExpiresIn: uint64(issuedToken.ExpiresAt.Sub(issuedToken.IssuedAt).Seconds()),
		IssuedAt:  issuedToken.IssuedAt.Format(time.RFC3339),
	}, nil
}
