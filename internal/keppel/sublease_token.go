// SPDX-FileCopyrightText: 2020-2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package keppel

import (
	"encoding/base64"
	"encoding/json"

	"github.com/sapcc/keppel/internal/models"
)

// SubleaseToken is the internal structure of a sublease token. Only the secret
// is passed on to the federation driver. The other attributes are only
// informational. GUIs/CLIs can display these data to the user for confirmation
// when the token is entered.
type SubleaseToken struct {
	AccountName     models.AccountName `json:"account"`
	PrimaryHostname string             `json:"primary"`
	Secret          string             `json:"secret"`
}

// Serialize returns the Base64-encoded JSON of this token. This is the format
// that gets passed to the user.
func (t SubleaseToken) Serialize() string {
	// only serialize SubleaseToken if it contains a secret at all
	if t.Secret == "" {
		return ""
	}

	buf, _ := json.Marshal(t)
	return base64.StdEncoding.EncodeToString(buf)
}

// ParseSubleaseToken constructs a SubleaseToken from its serialized form.
func ParseSubleaseToken(in string) (SubleaseToken, error) {
	if in == "" {
		// empty sublease token is acceptable for federation drivers that don't need one
		return SubleaseToken{}, nil
	}

	buf, err := base64.StdEncoding.DecodeString(in)
	if err != nil {
		return SubleaseToken{}, err
	}

	var t SubleaseToken
	err = json.Unmarshal(buf, &t)
	if err != nil {
		return SubleaseToken{}, err
	}
	return t, nil
}
