/******************************************************************************
*
*  Copyright 2018-2019 SAP SE
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

package tokenauth

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/docker/libtrust"
	"github.com/golang-jwt/jwt/v4"
	"github.com/sapcc/keppel/internal/keppel"
	uuid "github.com/satori/go.uuid"
)

//Service enumerates the different audiences for which we can issue tokens.
type Service int

const (
	//LocalService is the Service for tokens issued by this keppel-api for use
	//only with the same keppel-api.
	LocalService Service = 0
	//AnycastService is the Service for tokens issued by this keppel-api or one
	//of its peers for their shared anycast endpoint.
	AnycastService Service = 1
)

//Hostname returns the hostname that is used as the "audience" value in tokens
//and as the "service" value in auth challenges.
func (s Service) Hostname(cfg keppel.Configuration) string {
	switch s {
	case LocalService:
		return cfg.APIPublicURL.Hostname()
	case AnycastService:
		return cfg.AnycastAPIPublicURL.Hostname()
	default:
		panic(fmt.Sprintf("unknown auth service code: %d", s))
	}
}

//IssuerKey returns the issuer key that is used to sign tokens for this
//service.
func (s Service) IssuerKey(cfg keppel.Configuration) libtrust.PrivateKey {
	switch s {
	case LocalService:
		return cfg.JWTIssuerKey
	case AnycastService:
		return *cfg.AnycastJWTIssuerKey
	default:
		panic(fmt.Sprintf("unknown auth service code: %d", s))
	}
}

//Token represents a JWT (Java Web Token), as used for authenticating on the
//Registry v2 API.
type Token struct {
	//Authorization is the original Authorization object that is serialized within this token.
	Authorization keppel.Authorization
	//The service that this token can be used with.
	Audience Service
	//Access permissions for this token.
	Access []Scope
}

//TokenClaims is the type for JWT claims issued by Keppel.
type TokenClaims struct {
	jwt.StandardClaims
	Access                []Scope                      `json:"access"`
	EmbeddedAuthorization keppel.EmbeddedAuthorization `json:"kea"` //kea = keppel embedded authorization
}

//ParseTokenFromRequest tries to parse the Bearer token supplied in the
//request's Authorization header.
func ParseTokenFromRequest(r *http.Request, cfg keppel.Configuration, ad keppel.AuthDriver, audience Service) (*Token, *keppel.RegistryV2Error) {
	//read Authorization request header
	tokenStr := r.Header.Get("Authorization")
	if !strings.HasPrefix(tokenStr, "Bearer ") { //e.g. because it's missing
		return nil, keppel.ErrUnauthorized.With("no bearer token found in request headers")
	}
	tokenStr = strings.TrimPrefix(tokenStr, "Bearer ")

	//parse JWT
	var claims TokenClaims
	claims.EmbeddedAuthorization.AuthDriver = ad
	token, err := jwt.ParseWithClaims(tokenStr, &claims, func(t *jwt.Token) (interface{}, error) {
		//check that the signing method matches what we generate
		ourIssuerKey := audience.IssuerKey(cfg)
		ourSigningMethod := ChooseSigningMethod(ourIssuerKey)
		if !equalSigningMethods(ourSigningMethod, t.Method) {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
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

	return &Token{
		Authorization: claims.EmbeddedAuthorization.Authorization,
		Audience:      audience,
		Access:        claims.Access,
	}, nil
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

//ChooseSigningMethod returns the appropriate signing method for the given
//private key.
func ChooseSigningMethod(key libtrust.PrivateKey) jwt.SigningMethod {
	issuerKey := key.CryptoPrivateKey()
	switch issuerKey.(type) {
	case *ed25519.PrivateKey:
		return jwt.SigningMethodEdDSA
	case *ecdsa.PrivateKey:
		return jwt.SigningMethodES256
	case *rsa.PrivateKey:
		return jwt.SigningMethodRS256
	default:
		panic(fmt.Sprintf("do not know which JWT method to use for issuerKey.type = %T", issuerKey))
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

//IssuedToken is returned by Token.Issue().
type IssuedToken struct {
	SignedToken string
	IssuedAt    time.Time
	ExpiresAt   time.Time
}

//Issue generates a JWT for this Token instance.
func (t Token) Issue(cfg keppel.Configuration) (*IssuedToken, error) {
	now := time.Now()
	expiresIn := 4 * time.Hour //NOTE: could be made configurable if the need arises
	expiresAt := now.Add(expiresIn)

	issuerKey := t.Audience.IssuerKey(cfg)
	method := ChooseSigningMethod(issuerKey)

	publicHost := t.Audience.Hostname(cfg)
	token := jwt.NewWithClaims(method, TokenClaims{
		StandardClaims: jwt.StandardClaims{
			Id:        uuid.NewV4().String(),
			Audience:  publicHost,
			Issuer:    "keppel-api@" + cfg.APIPublicURL.Hostname(),
			Subject:   t.Authorization.UserName(),
			ExpiresAt: expiresAt.Unix(),
			NotBefore: now.Unix(),
			IssuedAt:  now.Unix(),
		},
		//access permissions granted to this token
		Access: t.Access,
		EmbeddedAuthorization: keppel.EmbeddedAuthorization{
			Authorization: t.Authorization,
		},
	})

	var (
		jwkMessage json.RawMessage
		err        error
	)
	jwkMessage, err = issuerKey.PublicKey().MarshalJSON()
	if err != nil {
		return nil, err
	}
	token.Header["jwk"] = &jwkMessage

	signed, err := token.SignedString(issuerKey.CryptoPrivateKey())
	return &IssuedToken{
		SignedToken: signed,
		ExpiresAt:   expiresAt,
		IssuedAt:    now,
	}, err
}
