// SPDX-FileCopyrightText: 2021 SAP SE
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/sapcc/keppel/internal/keppel"
)

// IncomingRequest describes everything we need to know about an incoming API
// request in order to check for Authorization.
type IncomingRequest struct {
	// The incoming request.
	HTTPRequest *http.Request
	// The required token scopes for this request. If the Authorization.ScopeSet
	// ends up not containing these scopes, the request is rejected and an auth
	// challenge is issued.
	Scopes ScopeSet
	// Whether anycast requests are acceptable on this endpoint.
	AllowsAnycast bool
	// Whether domain-remapped requests are acceptable on this endpoint.
	AllowsDomainRemapping bool
	// Filled when the user is trying to get a token from us. This enables basic
	// auth with username+password, and overrides the usual audience-sensing logic.
	AudienceForTokenIssuance *Audience
	// If this field is true, 403 is returned to indicate insufficient
	// authorization. Most APIs return 401 instead to ensure bug-for-bug
	// compatibility with Docker Registry.
	CorrectlyReturn403 bool
	// If true, Authorize() will not check if all requested scopes where
	// authorized.
	PartialAccessAllowed bool
	// If true, Authorize() will not assume an AnonymousUserIdentity when no auth
	// headers are provided. Users MUST present some sort of auth header.
	NoImplicitAnonymous bool
}

// Authorize checks if the given incoming request has a proper Authorization.
// If an error is returned, the given `errHeaders` must be added to the HTTP response.
//
// In addition to the accepted Authorization, we also return a Challenge,
// for when API implementations need to return custom 401 errors.
func (ir IncomingRequest) Authorize(ctx context.Context, cfg keppel.Configuration, ad keppel.AuthDriver, db *keppel.DB) (*Authorization, *Challenge, *keppel.RegistryV2Error) {
	r := ir.HTTPRequest

	// find audience
	var audience Audience
	if ir.AudienceForTokenIssuance != nil {
		audience = *ir.AudienceForTokenIssuance
	} else {
		u := keppel.OriginalRequestURL(r)
		audience = IdentifyAudience(u.Hostname(), cfg)

		// special case: an anycast request was explicitly reverse-proxied to our
		// non-anycast API by the keppel-api that originally received it
		forwardedBy := r.Header.Get("X-Keppel-Forwarded-By")
		if forwardedBy != "" {
			audience.IsAnycast = true
		}
	}

	// sanity checks
	if audience.IsAnycast {
		// completely forbid write operations on the anycast API (only the local API
		// may be used for writes and deletes)
		if r.Method != http.MethodHead && r.Method != http.MethodGet {
			msg := "write access is not supported for anycast requests"
			return nil, nil, keppel.ErrUnsupported.With(msg)
		}
		// only allow anycast usage when the API explicitly permits it
		if !ir.AllowsAnycast {
			msg := fmt.Sprintf("%s %s endpoint is not supported for anycast requests", r.Method, r.URL.Path)
			return nil, nil, keppel.ErrUnsupported.With(msg)
		}
	}
	if audience.AccountName != "" && !ir.AllowsDomainRemapping {
		msg := fmt.Sprintf("%s %s endpoint is not supported on domain-remapped APIs", r.Method, r.URL.Path)
		return nil, nil, keppel.ErrUnsupported.With(msg)
	}

	// if we return 401, this challenge will be used to render the Www-Authenticate header
	challenge := ir.buildAuthChallenge(cfg, audience)

	// obtain Authorization through one of the various supported methods
	var (
		authHeader     = r.Header.Get("Authorization")
		allowChallenge = false
		tokenFound     = false
		authz          *Authorization
	)
	switch {
	case strings.HasPrefix(authHeader, "Basic "):
		// clearly a request for basic auth
		if ir.AudienceForTokenIssuance == nil {
			// I'm being deliberately harsh with the wording of this error message
			// here; I've seen clients use basic auth on endpoints like GET /v2/ even
			// though that is completely nonsensical
			return nil, nil, challenge.AddTo(keppel.ErrUnauthorized.With("basic auth is not supported on this endpoint, your library's auth workflow is probably broken"))
		}
		uid, err := checkBasicAuth(ctx, authHeader, ad, db)
		if err != nil {
			return nil, nil, keppel.AsRegistryV2Error(err)
		}
		authz, err = ir.authorizeViaUserIdentity(uid, audience, db)
		if err != nil {
			return nil, nil, keppel.AsRegistryV2Error(err)
		}

	case strings.HasPrefix(authHeader, "Bearer "):
		// clearly a request for token auth
		var rerr *keppel.RegistryV2Error
		authz, rerr = parseToken(cfg, ad, audience, strings.TrimPrefix(authHeader, "Bearer "))
		if rerr != nil {
			return nil, nil, challenge.AddTo(rerr)
		}
		tokenFound = true
		allowChallenge = true

	case authHeader == "" || authHeader == "keppel":
		// possibly a request for driver auth, but fallback on AnonymousUserIdentity
		// if driver auth does not detect any matching headers
		uid, rerr := ad.AuthenticateUserFromRequest(r)
		if rerr != nil {
			return nil, nil, rerr
		}
		if uid == nil {
			switch {
			case authHeader == "keppel":
				// do not fallback if we were explicitly instructed to only use driver auth
				return nil, nil, keppel.ErrUnauthorized.With("no credentials found in request")
			case ir.NoImplicitAnonymous:
				return nil, nil, challenge.AddTo(keppel.ErrUnauthorized.With("no bearer token found in request headers"))
			default:
				uid = AnonymousUserIdentity
				allowChallenge = true
			}
		}

		var err error
		authz, err = ir.authorizeViaUserIdentity(uid, audience, db)
		if err != nil {
			return nil, nil, keppel.AsRegistryV2Error(err)
		}

	default:
		return nil, nil, errMalformedAuthHeader
	}

	// check if requested scope is covered by Authorization
	if !ir.PartialAccessAllowed {
		for _, scope := range ir.Scopes {
			// to ensure that GET /v2/ produces an auth challenge without any scopes,
			// we do not render InfoAPIScope into auth challenges; conversely, since
			// we don't challenge anyone to obtain tokens for InfoAPIScope, we need to
			// skip this scope here as well
			if InfoAPIScope.Contains(*scope) {
				continue
			}

			if !authz.ScopeSet.Contains(*scope) {
				// not covered -> generate error, possibly with auth challenge
				rerr := keppel.ErrUnauthorized.With("no bearer token found in request headers")
				if authz.UserIdentity.UserType() != keppel.AnonymousUser {
					if tokenFound {
						rerr = keppel.ErrDenied.With("token does not cover scope %s", scope)
					} else {
						rerr = keppel.ErrDenied.With("no permission for %s", scope)
					}
				}
				if allowChallenge {
					if tokenFound {
						rerr = challenge.WithErrorMessage("insufficient_scope").AddTo(rerr)
					} else {
						rerr = challenge.AddTo(rerr)
					}
				}
				if ir.CorrectlyReturn403 {
					rerr = rerr.WithStatus(http.StatusForbidden)
				}
				return nil, nil, rerr
			}
		}
	}

	return authz, &challenge, nil
}

func (ir IncomingRequest) buildAuthChallenge(cfg keppel.Configuration, audience Audience) Challenge {
	requestURL := keppel.OriginalRequestURL(ir.HTTPRequest)
	apiURL := &url.URL{Scheme: requestURL.Scheme, Host: requestURL.Host, Path: "keppel/v1/auth"}

	return Challenge{
		AuthEndpointURL:  apiURL.String(),
		AudienceHostname: audience.Hostname(cfg),
		Scopes:           ir.Scopes,
	}
}

var errMalformedAuthHeader = keppel.ErrUnauthorized.With("malformed Authorization header")

func checkBasicAuth(ctx context.Context, authHeader string, ad keppel.AuthDriver, db *keppel.DB) (keppel.UserIdentity, error) {
	// decode auth header into username/password pair
	bytes, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(authHeader, "Basic "))
	if err != nil {
		return nil, errMalformedAuthHeader
	}
	fields := strings.SplitN(string(bytes), ":", 2)
	if len(fields) != 2 {
		return nil, errMalformedAuthHeader
	}
	userName, password := fields[0], fields[1]

	// recognize peer credentials
	if strings.HasPrefix(userName, "replication@") {
		peerHostName := strings.TrimPrefix(userName, "replication@")
		peer, err := checkPeerCredentials(db, peerHostName, password)
		if err != nil {
			return nil, err
		}
		if peer == nil {
			return nil, keppel.ErrUnauthorized.With("invalid peer credentials")
		}
		return &PeerUserIdentity{PeerHostName: peerHostName}, nil
	}

	// recognize regular user credentials
	uid, rerr := ad.AuthenticateUser(ctx, userName, password)
	return uid, safelyReturnRegistryError(rerr)
}

func safelyReturnRegistryError(rerr *keppel.RegistryV2Error) error {
	// This looks stupid, but it ensures that a nil error value gets returned as
	// error(nil) instead of error(*keppel.RegistryV2Error(nil)). These are very
	//different: The former is an untyped nil, and the latter is a typed nil.
	// Since these are different, a typed nil would not match `err == nil`.
	if rerr == nil {
		return nil
	}
	return rerr
}

func (ir IncomingRequest) authorizeViaUserIdentity(uid keppel.UserIdentity, audience Audience, db *keppel.DB) (*Authorization, error) {
	ss, err := filterAuthorized(ir, uid, audience, db)
	if err != nil {
		return nil, err
	}

	return &Authorization{
		UserIdentity: uid,
		Audience:     audience,
		ScopeSet:     ss,
	}, nil
}
