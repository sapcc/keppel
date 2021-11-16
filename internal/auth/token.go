/*******************************************************************************
*
* Copyright 2018-2021 SAP SE
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
	"crypto"
	"crypto/ed25519"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/sapcc/keppel/internal/keppel"
	uuid "github.com/satori/go.uuid"
)

//Type representation for JWT claims issued by Keppel.
type tokenClaims struct {
	jwt.StandardClaims
	Access   []Scope              `json:"access"`
	Embedded embeddedUserIdentity `json:"kea"` //kea = keppel embedded authorization ("UserIdentity" used to be called "Authorization")
}

func parseToken(cfg keppel.Configuration, ad keppel.AuthDriver, audience Service, tokenStr string) (*Authorization, *keppel.RegistryV2Error) {
	//parse JWT
	var claims tokenClaims
	claims.Embedded.AuthDriver = ad
	token, err := jwt.ParseWithClaims(tokenStr, &claims, func(t *jwt.Token) (interface{}, error) {
		//check that the signing method matches what we generate
		ourIssuerKey := audience.IssuerKey(cfg)
		ourSigningMethod := chooseSigningMethod(ourIssuerKey)
		if !equalSigningMethods(ourSigningMethod, t.Method) {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}

		//jwt.Parse needs the public key for our issuer key to validate the token
		return derivePublicKey(ourIssuerKey), nil
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
	publicHost := audience.Hostname(cfg)
	if audience == LocalService {
		if !claims.StandardClaims.VerifyIssuer("keppel-api@"+publicHost, true) {
			return nil, keppel.ErrUnauthorized.With("token has wrong issuer (expected keppel-api@%s)", publicHost)
		}
		//NOTE: For anycast tokens, we don't verify the issuer. Any of our peers
		//could have issued the token.
	}
	if !claims.StandardClaims.VerifyAudience(publicHost, true) {
		return nil, keppel.ErrUnauthorized.With("token has wrong audience (expected %s)", publicHost)
	}

	var ss ScopeSet
	for _, scope := range claims.Access {
		ss.Add(scope)
	}
	return &Authorization{
		UserIdentity: claims.Embedded.UserIdentity,
		ScopeSet:     ss,
		Service:      audience,
	}, nil
}

//TokenResponse is the format expected by Docker in an auth response. The Token
//field contains a Java Web Token (JWT).
type TokenResponse struct {
	Token     string `json:"token"`
	ExpiresIn uint64 `json:"expires_in"`
	IssuedAt  string `json:"issued_at"`
}

//IssueToken renders the given Authorization into a JWT token that can be used
//as a Bearer token to authenticate on Keppel's various APIs.
func (a Authorization) IssueToken(cfg keppel.Configuration) (*TokenResponse, error) {
	now := time.Now()
	expiresIn := 4 * time.Hour //NOTE: could be made configurable if the need arises
	expiresAt := now.Add(expiresIn)

	issuerKey := a.Service.IssuerKey(cfg)
	method := chooseSigningMethod(issuerKey)

	publicHost := a.Service.Hostname(cfg)
	token, err := jwt.NewWithClaims(method, tokenClaims{
		StandardClaims: jwt.StandardClaims{
			Id:        uuid.NewV4().String(),
			Audience:  publicHost,
			Issuer:    "keppel-api@" + cfg.APIPublicURL.Hostname(),
			Subject:   a.UserIdentity.UserName(),
			ExpiresAt: expiresAt.Unix(),
			NotBefore: now.Unix(),
			IssuedAt:  now.Unix(),
		},
		//access permissions granted to this token
		Access:   a.ScopeSet.Flatten(),
		Embedded: embeddedUserIdentity{UserIdentity: a.UserIdentity},
	}).SignedString(issuerKey)
	return &TokenResponse{
		Token:     token,
		ExpiresIn: uint64(expiresAt.Sub(now).Seconds()),
		IssuedAt:  now.Format(time.RFC3339),
	}, err
}

func chooseSigningMethod(key crypto.PrivateKey) jwt.SigningMethod {
	switch key.(type) {
	case ed25519.PrivateKey:
		return jwt.SigningMethodEdDSA
	case *rsa.PrivateKey:
		return jwt.SigningMethodRS256
	default:
		panic(fmt.Sprintf("do not know which JWT method to use for issuerKey.type = %T", key))
	}
}

func derivePublicKey(key crypto.PrivateKey) crypto.PublicKey {
	switch key := key.(type) {
	case ed25519.PrivateKey:
		return key.Public()
	case *rsa.PrivateKey:
		return key.Public()
	default:
		panic(fmt.Sprintf("do not know which JWT method to use for issuerKey.type = %T", key))
	}
}

func equalSigningMethods(m1, m2 jwt.SigningMethod) bool {
	switch m1 := m1.(type) {
	case *jwt.SigningMethodEd25519:
		if m2, ok := m2.(*jwt.SigningMethodEd25519); ok {
			return *m1 == *m2
		}
		return false
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
		panic(fmt.Sprintf("do not know how to compare signing methods of type %T", m1))
	}
}

////////////////////////////////////////////////////////////////////////////////
// type embeddedUserIdentity

//Wraps an UserIdentity such that it can be serialized into JSON.
type embeddedUserIdentity struct {
	UserIdentity keppel.UserIdentity
	//AuthDriver is ignored during serialization, but must be filled prior to
	//deserialization because some types of UserIdentity require their
	//respective AuthDriver to deserialize properly.
	AuthDriver keppel.AuthDriver
}

//MarshalJSON implements the json.Marshaler interface.
func (e embeddedUserIdentity) MarshalJSON() ([]byte, error) {
	typeName, payload, err := e.UserIdentity.SerializeToJSON()
	if err != nil {
		return nil, err
	}

	//The straight-forward approach would be to serialize as
	//`{"type":"foo","payload":"something"}`, but we serialize as
	//`{"foo":"something"}` instead to shave off a few bytes.
	return json.Marshal(map[string]json.RawMessage{typeName: json.RawMessage(payload)})
}

//UnmarshalJSON implements the json.Marshaler interface.
func (e *embeddedUserIdentity) UnmarshalJSON(in []byte) error {
	if e.AuthDriver == nil {
		return errors.New("cannot unmarshal EmbeddedAuthorization without an AuthDriver")
	}

	m := make(map[string]json.RawMessage)
	err := json.Unmarshal(in, &m)
	if err != nil {
		return err
	}
	if len(m) != 1 {
		return fmt.Errorf("cannot unmarshal EmbeddedAuthorization with %d components", len(m))
	}

	for typeName, payload := range m {
		e.UserIdentity, err = keppel.DeserializeUserIdentity(typeName, []byte(payload), e.AuthDriver)
		return err
	}

	//the loop body executes exactly once, therefore this location is unreachable
	panic("unreachable")
}
