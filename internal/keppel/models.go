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

package keppel

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"
	"time"

	gorp "gopkg.in/gorp.v2"
)

//Account contains a record from the `accounts` table.
type Account struct {
	Name               string `db:"name"`
	AuthTenantID       string `db:"auth_tenant_id"`
	RegistryHTTPSecret string `db:"registry_http_secret"`
	//UpstreamPeerHostName is set if and only if the "on_first_use" replication strategy is used.
	UpstreamPeerHostName string `db:"upstream_peer_hostname"`
}

//SwiftContainerName returns the name of the Swift container backing this
//Keppel account.
func (a Account) SwiftContainerName() string {
	return "keppel-" + a.Name
}

//PostgresDatabaseName returns the name of the Postgres database which contains this
//Keppel account's metadata.
func (a Account) PostgresDatabaseName() string {
	return "keppel_" + strings.Replace(a.Name, "-", "_", -1)
}

//GenerateRegistryHTTPSecret generates a value for the Account.RegistryHTTPSecret field.
func GenerateRegistryHTTPSecret() string {
	var buf [36]byte
	_, err := rand.Read(buf[:])
	if err != nil {
		panic(err.Error())
	}
	return base64.StdEncoding.EncodeToString(buf[:])
}

//FindAccount works similar to db.SelectOne(), but returns nil instead of
//sql.ErrNoRows if no account exists with this name.
func (db *DB) FindAccount(name string) (*Account, error) {
	var account Account
	err := db.SelectOne(&account,
		"SELECT * FROM accounts WHERE name = $1", name)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &account, err
}

//AllAccounts implements the DBAccessForOrchestrationDriver interface.
func (db *DB) AllAccounts() ([]Account, error) {
	var accounts []Account
	_, err := db.Select(&accounts, `SELECT * FROM accounts`)
	if err != nil {
		return nil, err
	}

	//when upgrading DBs before schema version 3, some RegistryHTTPSecret
	//fields will be empty -> fill them now
	hasMissingSecrets := false
	for _, account := range accounts {
		if account.RegistryHTTPSecret == "" {
			hasMissingSecrets = true
			secret := GenerateRegistryHTTPSecret()
			//someone else might be doing the same, so only update when we're the
			//first to add a secret
			_, err := db.Exec(`UPDATE accounts SET registry_http_secret = $1 WHERE name = $2 AND registry_http_secret = ''`,
				secret, account.Name)
			if err != nil {
				return nil, err
			}
		}
	}

	if hasMissingSecrets {
		//now all accounts have secrets, but they may not be the ones generated by
		//us - restart the `SELECT * FROM accounts` call
		return db.AllAccounts()
	}

	return accounts, nil
}

////////////////////////////////////////////////////////////////////////////////

//RBACPolicy contains a record from the `rbac_policies` table.
type RBACPolicy struct {
	AccountName        string `db:"account_name"`
	RepositoryPattern  string `db:"match_repository"`
	UserNamePattern    string `db:"match_username"`
	CanPullAnonymously bool   `db:"can_anon_pull"`
	CanPull            bool   `db:"can_pull"`
	CanPush            bool   `db:"can_push"`
	CanDelete          bool   `db:"can_delete"`
}

//Matches evaluates the regexes in this policy.
func (r RBACPolicy) Matches(repoName, userName string) bool {
	if r.RepositoryPattern != "" {
		rx, err := regexp.Compile(fmt.Sprintf(`^%s/%s$`,
			regexp.QuoteMeta(r.AccountName),
			r.RepositoryPattern,
		))
		if err != nil || !rx.MatchString(repoName) {
			return false
		}
	}

	if r.UserNamePattern != "" {
		rx, err := regexp.Compile(fmt.Sprintf(`^%s$`, r.UserNamePattern))
		if err != nil || !rx.MatchString(userName) {
			return false
		}
	}

	return true
}

////////////////////////////////////////////////////////////////////////////////

//Repository contains a record from the `repos` table.
type Repository struct {
	ID          int64  `db:"id"`
	AccountName string `db:"account_name"`
	Name        string `db:"name"`
}

//FindOrCreateRepository works similar to db.SelectOne(), but autovivifies a
//Repository record when none exists yet.
func (db *DB) FindOrCreateRepository(name string, account Account) (*Repository, error) {
	repo, err := db.FindRepository(name, account)
	if err == sql.ErrNoRows {
		repo = &Repository{
			AccountName: account.Name,
			Name:        name,
		}
		err = db.Insert(repo)
	}
	return repo, err
}

//FindRepository is a convenience wrapper around db.SelectOne(). If the
//repository in question does not exist, sql.ErrNoRows is returned.
func (db *DB) FindRepository(name string, account Account) (*Repository, error) {
	var repo Repository
	err := db.SelectOne(&repo,
		"SELECT * FROM repos WHERE account_name = $1 AND name = $2", account.Name, name)
	return &repo, err
}

//FullName prepends the account name to the repository name.
func (r Repository) FullName() string {
	return r.AccountName + `/` + r.Name
}

////////////////////////////////////////////////////////////////////////////////

//Manifest contains a record from the `manifests` table.
type Manifest struct {
	RepositoryID int64     `db:"repo_id"`
	Digest       string    `db:"digest"`
	MediaType    string    `db:"media_type"`
	SizeBytes    uint64    `db:"size_bytes"`
	PushedAt     time.Time `db:"pushed_at"`
}

//InsertIfMissing is equivalent to `e.Insert(&m)`, but does not fail if the
//manifest exists in the database already.
func (m Manifest) InsertIfMissing(e gorp.SqlExecutor) error {
	_, err := e.Exec(`
		INSERT INTO manifests (repo_id, digest, media_type, size_bytes, pushed_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (repo_id, digest) DO NOTHING
	`, m.RepositoryID, m.Digest, m.MediaType, m.SizeBytes, m.PushedAt)
	return err
}

////////////////////////////////////////////////////////////////////////////////

//Tag contains a record from the `tags` table.
type Tag struct {
	RepositoryID int64     `db:"repo_id"`
	Name         string    `db:"name"`
	Digest       string    `db:"digest"`
	PushedAt     time.Time `db:"pushed_at"`
}

//InsertIfMissing is equivalent to `e.Insert(&m)`, but does not fail if the
//manifest exists in the database already.
func (t Tag) InsertIfMissing(e gorp.SqlExecutor) error {
	_, err := e.Exec(`
		INSERT INTO tags (repo_id, name, digest, pushed_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (repo_id, name) DO UPDATE
			SET digest = EXCLUDED.digest, pushed_at = EXCLUDED.pushed_at
	`, t.RepositoryID, t.Name, t.Digest, t.PushedAt)
	return err
}

////////////////////////////////////////////////////////////////////////////////

//Quotas contains a record from the `quotas` table.
type Quotas struct {
	AuthTenantID  string `db:"auth_tenant_id"`
	ManifestCount uint64 `db:"manifests"`
}

//FindQuotas works similar to db.SelectOne(), but returns nil instead of
//sql.ErrNoRows if no quota set exists for this auth tenant.
func (db *DB) FindQuotas(authTenantID string) (*Quotas, error) {
	var quotas Quotas
	err := db.SelectOne(&quotas,
		"SELECT * FROM quotas WHERE auth_tenant_id = $1", authTenantID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &quotas, err
}

//DefaultQuotas creates a new Quotas instance with the default quotas.
func DefaultQuotas(authTenantID string) *Quotas {
	//Right now, the default quota is always 0. The value of having this function
	//is to ensure that we only need to change this place if this ever changes.
	return &Quotas{
		AuthTenantID:  authTenantID,
		ManifestCount: 0,
	}
}

var manifestUsageQuery = `
	SELECT COUNT(m.digest)
	  FROM manifests m
	  JOIN repos r ON m.repo_id = r.id
	  JOIN accounts a ON a.name = r.account_name
	 WHERE a.auth_tenant_id = $1
`

//GetManifestUsage returns how many manifests currently exist in repos in
//accounts connected to this quota set's auth tenant.
func (q Quotas) GetManifestUsage(db gorp.SqlExecutor) (uint64, error) {
	manifestCount, err := db.SelectInt(manifestUsageQuery, q.AuthTenantID)
	return uint64(manifestCount), err
}

////////////////////////////////////////////////////////////////////////////////

//Peer contains a record from the `peers` table.
type Peer struct {
	HostName string `db:"hostname"`

	//OurPassword is what we use to log in at the peer.
	OurPassword string `db:"our_password"`

	//TheirCurrentPasswordHash and TheirPreviousPasswordHash is what the peer
	//uses to log in with us. Passwords are rotated hourly. We allow access with
	//the current *and* the previous password to avoid a race where we enter the
	//new password in the database and then reject authentication attempts from
	//the peer before we told them about the new password.
	TheirCurrentPasswordHash  string `db:"their_current_password_hash"`
	TheirPreviousPasswordHash string `db:"their_previous_password_hash"`

	//LastPeeredAt is when we last issued a new password for this peer.
	LastPeeredAt *time.Time `db:"last_peered_at"`
}

////////////////////////////////////////////////////////////////////////////////

//PendingBlob contains a record from the `pending_blobs` table.
type PendingBlob struct {
	RepositoryID int64         `db:"repo_id"`
	Digest       string        `db:"digest"`
	Reason       PendingReason `db:"reason"`
	PendingSince time.Time     `db:"since"`
}

//PendingManifest contains a record from the `pending_manifests` table.
type PendingManifest struct {
	RepositoryID int64         `db:"repo_id"`
	Reference    string        `db:"reference"` //either digest or tag
	Digest       string        `db:"digest"`
	Reason       PendingReason `db:"reason"`
	PendingSince time.Time     `db:"since"`
	MediaType    string        `db:"media_type"`
	Content      string        `db:"content"`
}

//PendingReason is an enum that explains why a blob or manifest is pending.
type PendingReason string

const (
	//PendingBecauseOfReplication is when a blob or manifest is pending because
	//it is currently being replicated from an upstream registry.
	PendingBecauseOfReplication PendingReason = "replication"
)

////////////////////////////////////////////////////////////////////////////////

func initModels(db *gorp.DbMap) {
	db.AddTableWithName(Account{}, "accounts").SetKeys(false, "name")
	db.AddTableWithName(RBACPolicy{}, "rbac_policies").SetKeys(false, "account_name", "match_repository", "match_username")
	db.AddTableWithName(Repository{}, "repos").SetKeys(true, "id")
	db.AddTableWithName(Manifest{}, "manifests").SetKeys(false, "repo_id", "digest")
	db.AddTableWithName(Tag{}, "tags").SetKeys(false, "repo_id", "name")
	db.AddTableWithName(Quotas{}, "quotas").SetKeys(false, "auth_tenant_id")
	db.AddTableWithName(Peer{}, "peers").SetKeys(false, "hostname")
	db.AddTableWithName(PendingBlob{}, "pending_blobs").SetKeys(false, "repo_id", "digest")
	db.AddTableWithName(PendingManifest{}, "pending_manifests").SetKeys(false, "repo_id", "reference")
}
