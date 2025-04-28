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

package tasks

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/go-gorp/gorp/v3"
	"github.com/opencontainers/go-digest"
	"github.com/sapcc/go-bits/logg"

	authapi "github.com/sapcc/keppel/internal/api/auth"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
)

// IssueNewPasswordForPeer issues a new replication password for the given peer.
//
// The `tx` argument can be given if the caller already has a transaction open
// for this operation. This is useful because it is the caller's responsibility
// to lock the database row for the peer to prevent concurrent issuances for the
// same peer by different keppel-api instances.
func IssueNewPasswordForPeer(ctx context.Context, cfg keppel.Configuration, db *keppel.DB, tx *gorp.Transaction, peer models.Peer) (resultErr error) {
	newPasswordBytes := make([]byte, 20)
	_, err := rand.Read(newPasswordBytes)
	if err != nil {
		return err
	}
	newPassword := hex.EncodeToString(newPasswordBytes)

	// NOTE: We acknowledge that it's usually not good practice to hash passwords with SHA-256.
	// In fact, we used to use BCrypt here, but we replaced it because it consumed 80% of the CPU time on our API processes, just for checking peer credentials!
	//
	// We find the choice of SHA-2 acceptable here because the peer passwords have:
	// a) extremely high entropy compared to passwords used by human users (20 bytes = 160 bits)
	// b) extremely short lifetime (10 minutes per renewal, and effectively 20 minutes total because we accept the previous password, too)
	//
	// Even if an attacker could run, say, 1 terahash per second, for SHA-256, they would take >1e+28 years to get through 160 bits of entropy.
	newPasswordHashed := digest.SHA256.FromString(newPassword).String()

	// update password in our own DB - we need to do this first because, as soon
	// as we send the HTTP request, the peer could come back to us at any time to
	// verify the password
	_, err = tx.Exec(`
		UPDATE peers SET
			their_current_password_hash = $1,
			their_previous_password_hash = their_current_password_hash,
			last_peered_at = NOW()
		WHERE hostname = $2
	`, newPasswordHashed, peer.HostName)
	if err == nil {
		err = tx.Commit()
	} else {
		errRollback := tx.Rollback()
		if errRollback != nil {
			logg.Error("unexpected error during SQL ROLLBACK: " + errRollback.Error())
		}
	}
	if err != nil {
		return fmt.Errorf("error while issuing new password for peer: %w", err)
	}

	// the problem is that, if we later find that the peer has not successfully
	// stored the password on their side, we need to revert these changes,
	// otherwise the actual credentials used by the peer rotate out of our DB
	resultErr = errors.New("interrupted")
	defer func() {
		if resultErr == nil {
			return
		}
		_, err := db.Exec(`
			UPDATE peers SET
				their_current_password_hash = $1,
				their_previous_password_hash = $2,
				last_peered_at = $3
			WHERE hostname = $4
		`, peer.TheirCurrentPasswordHash, peer.TheirPreviousPasswordHash,
			peer.LastPeeredAt, peer.HostName)
		if err != nil {
			resultErr = fmt.Errorf("%s (additional error encountered while attempting to rollback the new peer password in our DB: %s)", resultErr.Error(), err.Error())
		}
	}()

	// send new credentials to peer
	bodyBytes, _ := json.Marshal(authapi.PeeringRequest{
		PeerHostName: cfg.APIPublicHostname,
		UserName:     "replication@" + peer.HostName,
		Password:     newPassword,
	})
	peerURL := fmt.Sprintf("https://%s/keppel/v1/auth/peering", peer.HostName)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, peerURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return &url.Error{
			Op:  "Post",
			URL: peerURL,
			Err: fmt.Errorf("expected 204 No Content, but got %s", resp.Status),
		}
	}

	return nil
}
