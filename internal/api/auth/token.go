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
	"encoding/json"
	"time"

	jwt "github.com/dgrijalva/jwt-go"
	"github.com/sapcc/keppel/internal/auth"
	"github.com/sapcc/keppel/internal/keppel"
	uuid "github.com/satori/go.uuid"
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
	now := time.Now()
	expiresIn := 1 * time.Hour //NOTE: could be made configurable if the need arises
	expiry := now.Add(expiresIn)

	issuerKey := cfg.JWTIssuerKey
	method := auth.ChooseSigningMethod(issuerKey)

	publicHost := cfg.APIPublicHostname()
	token := jwt.NewWithClaims(method, auth.TokenClaims{
		StandardClaims: jwt.StandardClaims{
			Id:        uuid.NewV4().String(),
			Audience:  t.Audience,
			Issuer:    "keppel-api@" + publicHost,
			Subject:   t.UserName,
			ExpiresAt: expiry.Unix(),
			NotBefore: now.Unix(),
			IssuedAt:  now.Unix(),
		},
		//access permissions granted to this token
		Access: t.Access,
	})

	var (
		jwkMessage json.RawMessage
		err        error
	)
	jwkMessage, err = cfg.JWTIssuerKey.PublicKey().MarshalJSON()
	if err != nil {
		return nil, err
	}
	token.Header["jwk"] = &jwkMessage

	signed, err := token.SignedString(issuerKey.CryptoPrivateKey())
	return &TokenResponse{
		Token:     signed,
		ExpiresIn: uint64(expiresIn.Seconds()),
		IssuedAt:  now.Format(time.RFC3339),
	}, err
}
