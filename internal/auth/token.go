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

package auth

import (
	"crypto/ecdsa"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	jwt "github.com/dgrijalva/jwt-go"
	"github.com/docker/libtrust"
	"github.com/sapcc/keppel/internal/keppel"
	uuid "github.com/satori/go.uuid"
)

//Token represents a JWT (Java Web Token), as used for authenticating on the
//Registry v2 API.
type Token struct {
	//The name of this user who created this token.
	UserName string
	//Access permissions for this token.
	Access []Scope
	//ListableAccounts is only set when Access contains "registy:catalog:*", and
	//identifies the accounts that may be listed by the user of this token.
	ListableAccounts []string
}

//Contains returns true if the given token authorizes the user for this scope.
func (t Token) Contains(s Scope) bool {
	for _, scope := range t.Access {
		if scope.Contains(s) {
			return true
		}
	}
	return false
}

type tokenClaims struct {
	jwt.StandardClaims
	Access []Scope `json:"access"`
}

//ParseTokenFromRequest tries to parse the Bearer token supplied in the
//request's Authorization header.
func ParseTokenFromRequest(r *http.Request) (*Token, *keppel.RegistryV2Error) {
	//read Authorization request header
	tokenStr := r.Header.Get("Authorization")
	if !strings.HasPrefix(tokenStr, "Bearer ") { //e.g. because it's missing
		return nil, keppel.ErrUnauthorized.With("no bearer token found in request headers")
	}
	tokenStr = strings.TrimPrefix(tokenStr, "Bearer ")

	//parse JWT
	var claims tokenClaims
	token, err := jwt.ParseWithClaims(tokenStr, &claims, func(t *jwt.Token) (interface{}, error) {
		//check that the signing method matches what we generate
		ourIssuerKey := keppel.State.JWTIssuerKey
		ourSigningMethod := chooseSigningMethod(ourIssuerKey)
		if !equalSigningMethods(ourSigningMethod, t.Method) {
			return nil, fmt.Errorf("Unexpected signing method: %v", t.Header["alg"])
		}

		//jwt.Parse needs the public key for our issuer key to validate the token
		return ourIssuerKey.PublicKey().CryptoPublicKey(), nil
	})
	if err != nil {
		return nil, keppel.ErrUnauthorized.With(err.Error())
	}
	if !token.Valid {
		//NOTE: This branch is defense in depth. As of the time of this writing,
		//token.Valid == false if and only if err != nil.
		return nil, keppel.ErrUnauthorized.With("token invalid")
	}

	//check claims (allow up to 3 seconds clock mismatch)
	now := time.Now().Unix()
	if !claims.StandardClaims.VerifyExpiresAt(now-3, true) {
		return nil, keppel.ErrUnauthorized.With("token expired")
	}
	if !claims.StandardClaims.VerifyNotBefore(now+3, true) {
		return nil, keppel.ErrUnauthorized.With("token not valid yet")
	}
	publicHost := keppel.State.Config.APIPublicHostname()
	if !claims.StandardClaims.VerifyIssuer("keppel-api@"+publicHost, true) {
		return nil, keppel.ErrUnauthorized.With("token has wrong issuer")
	}
	if !claims.StandardClaims.VerifyAudience(publicHost, true) {
		return nil, keppel.ErrUnauthorized.With("token has wrong audience")
	}

	return &Token{
		UserName: claims.StandardClaims.Subject,
		Access:   claims.Access,
	}, nil
}

//IncludesAccessTo checks if this token permits access to the given resource
//with the given action.
func (t Token) IncludesAccessTo(resourceType, resourceName, action string) bool {
	for _, scope := range t.Access {
		if scope.ResourceType == resourceType && scope.ResourceName == resourceName {
			for _, a := range scope.Actions {
				if a == action {
					return true
				}
			}
		}
	}
	return false
}

//TokenResponse is the format expected by Docker in an auth response. The Token
//field contains a Java Web Token (JWT).
type TokenResponse struct {
	Token     string `json:"token"`
	ExpiresIn uint64 `json:"expires_in"`
	IssuedAt  string `json:"issued_at"`
}

//ToResponse renders this token as a Java Web Token and returns a JSON-serializable
//struct in the format expected by Docker in an auth response.
func (t Token) ToResponse() (*TokenResponse, error) {
	now := time.Now()
	expiresIn := 1 * time.Hour //TODO make configurable?
	expiry := now.Add(expiresIn)

	issuerKey := keppel.State.JWTIssuerKey
	method := chooseSigningMethod(issuerKey)

	publicHost := keppel.State.Config.APIPublicHostname()
	token := jwt.NewWithClaims(method, tokenClaims{
		StandardClaims: jwt.StandardClaims{
			Id: uuid.NewV4().String(),
			//audience must match "service" argument from request
			Audience:  publicHost,
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
	jwkMessage, err = keppel.State.JWTIssuerKey.PublicKey().MarshalJSON()
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

func chooseSigningMethod(key libtrust.PrivateKey) jwt.SigningMethod {
	issuerKey := key.CryptoPrivateKey()
	switch issuerKey.(type) {
	case *ecdsa.PrivateKey:
		return jwt.SigningMethodES256
	case *rsa.PrivateKey:
		return jwt.SigningMethodRS256
	default:
		panic(fmt.Sprintf("do not know which JWT method to use for issuerKey.type = %t", issuerKey))
	}
}

func equalSigningMethods(m1, m2 jwt.SigningMethod) bool {
	switch m1 := m1.(type) {
	case *jwt.SigningMethodECDSA:
		if m2, ok := m2.(*jwt.SigningMethodECDSA); ok {
			return *m1 == *m2
		}
		return false
	case *jwt.SigningMethodRSA:
		if m2, ok := m2.(*jwt.SigningMethodRSA); ok {
			return *m1 == *m2
		}
		return false
	default:
		panic(fmt.Sprintf("do not know how to compare signing methods of type %t", m1))
	}
}
