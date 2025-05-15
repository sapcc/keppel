// SPDX-FileCopyrightText: 2024 SAP SE
// SPDX-License-Identifier: Apache-2.0

package keppel

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/sapcc/keppel/internal/models"
)

// ReplicationPolicy represents a replication policy in the API.
type ReplicationPolicy struct {
	Strategy ReplicationStrategy `json:"strategy"`
	// only for `on_first_use`
	UpstreamPeerHostName string `json:"upstream_peer_hostname"`
	// only for `from_external_on_first_use`
	ExternalPeer ReplicationExternalPeerSpec `json:"external_peer"`
}

// ReplicationStrategy is an enum that appears in type ReplicationPolicy.
type ReplicationStrategy string

const (
	NoReplicationStrategy          ReplicationStrategy = ""
	OnFirstUseStrategy             ReplicationStrategy = "on_first_use"
	FromExternalOnFirstUseStrategy ReplicationStrategy = "from_external_on_first_use"
)

// ReplicationExternalPeerSpec appears in type ReplicationPolicy.
type ReplicationExternalPeerSpec struct {
	URL      string `json:"url"`
	UserName string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

// MarshalJSON implements the json.Marshaler interface.
func (r ReplicationPolicy) MarshalJSON() ([]byte, error) {
	switch r.Strategy {
	case OnFirstUseStrategy:
		data := struct {
			Strategy             ReplicationStrategy `json:"strategy"`
			UpstreamPeerHostName string              `json:"upstream"`
		}{r.Strategy, r.UpstreamPeerHostName}
		return json.Marshal(data)
	case FromExternalOnFirstUseStrategy:
		data := struct {
			Strategy     ReplicationStrategy         `json:"strategy"`
			ExternalPeer ReplicationExternalPeerSpec `json:"upstream"`
		}{r.Strategy, r.ExternalPeer}
		return json.Marshal(data)
	default:
		return nil, fmt.Errorf("do not know how to serialize ReplicationPolicy with strategy %q", r.Strategy)
	}
}

// UnmarshalJSON implements the json.Unmarshaler interface.
func (r *ReplicationPolicy) UnmarshalJSON(buf []byte) error {
	var s struct {
		Strategy ReplicationStrategy `json:"strategy"`
		Upstream json.RawMessage     `json:"upstream"`
	}
	err := json.Unmarshal(buf, &s)
	if err != nil {
		return err
	}
	r.Strategy = s.Strategy

	if len(s.Upstream) == 0 {
		// need a more explicit error for this, otherwise the next json.Unmarshal()
		// will return a relatively inscrutable "unexpected end of JSON input"
		return errors.New(`missing field "upstream" in ReplicationPolicy`)
	}

	switch r.Strategy {
	case OnFirstUseStrategy:
		return json.Unmarshal(s.Upstream, &r.UpstreamPeerHostName)
	case FromExternalOnFirstUseStrategy:
		return json.Unmarshal(s.Upstream, &r.ExternalPeer)
	default:
		return fmt.Errorf("do not know how to deserialize ReplicationPolicy with strategy %q", r.Strategy)
	}
}

// RenderReplicationPolicy builds a ReplicationPolicy object out of the
// information in the given account model.
func RenderReplicationPolicy(account models.Account) *ReplicationPolicy {
	if account.UpstreamPeerHostName != "" {
		return &ReplicationPolicy{
			Strategy:             OnFirstUseStrategy,
			UpstreamPeerHostName: account.UpstreamPeerHostName,
		}
	}

	if account.ExternalPeerURL != "" {
		return &ReplicationPolicy{
			Strategy: FromExternalOnFirstUseStrategy,
			ExternalPeer: ReplicationExternalPeerSpec{
				URL:      account.ExternalPeerURL,
				UserName: account.ExternalPeerUserName,
				//NOTE: Password is omitted here for security reasons
			},
		}
	}

	return nil
}

var (
	ErrIncompatibleReplicationPolicy = errors.New("cannot change replication policy on existing account")
)

// ApplyToAccount validates this policy and stores it in the given account model.
//
// WARNING 1: For existing accounts, the caller must ensure that the policy uses
// the same replication strategy as the given account already does.
//
// WARNING 2: For internal replica accounts, the caller must ensure that the
// UpstreamPeerHostName refers to a known peer. This method does not do it
// itself because callers often need to do other things with the peer, too.
func (r ReplicationPolicy) ApplyToAccount(account *models.Account) error {
	switch r.Strategy {
	case OnFirstUseStrategy:
		if account.UpstreamPeerHostName == "" {
			// on new accounts, accept any upstream peer
			account.UpstreamPeerHostName = r.UpstreamPeerHostName
		} else if account.UpstreamPeerHostName != r.UpstreamPeerHostName {
			// on existing accounts, changing the upstream peer is not allowed
			return ErrIncompatibleReplicationPolicy
		}

	case FromExternalOnFirstUseStrategy:
		rerr := r.ExternalPeer.applyToAccount(account)
		if rerr != nil {
			return rerr
		}

	default:
		return fmt.Errorf("strategy %s is unsupported", r.Strategy)
	}

	return nil
}

func (r ReplicationExternalPeerSpec) applyToAccount(account *models.Account) error {
	// peer URL must be given for new accounts, and stay consistent for existing accounts
	if r.URL == "" {
		return errors.New(`missing upstream URL for "from_external_on_first_use" replication`)
	}
	isNewAccount := account.ExternalPeerURL == ""
	if isNewAccount {
		account.ExternalPeerURL = r.URL
	} else if account.ExternalPeerURL != r.URL {
		return ErrIncompatibleReplicationPolicy
	}

	// on existing accounts, having only a username is acceptable if it's unchanged
	// (this case occurs when a client GETs the account, changes something not related
	// to replication, and PUTs the result; the password is redacted in GET)
	if !isNewAccount && r.UserName != "" && r.Password == "" {
		if r.UserName == account.ExternalPeerUserName {
			r.Password = account.ExternalPeerPassword // to save it from being overwritten below
		} else {
			return errors.New(`cannot change username for "from_external_on_first_use" replication without also changing password`)
		}
	}

	// pull credentials can be updated mostly at will
	if (r.UserName == "") != (r.Password == "") {
		return errors.New(`need either both username and password or neither for "from_external_on_first_use" replication`)
	}
	account.ExternalPeerUserName = r.UserName
	account.ExternalPeerPassword = r.Password
	return nil
}
