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

package redis

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/sapcc/go-bits/osext"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
)

type federationDriver struct {
	ownHostname string
	prefix      string
	rc          *redis.Client
}

func init() {
	keppel.FederationDriverRegistry.Add(func() keppel.FederationDriver { return &federationDriver{} })
}

// PluginTypeID implements the keppel.FederationDriver interface.
func (d *federationDriver) PluginTypeID() string { return "redis" }

// Init implements the keppel.FederationDriver interface.
func (d *federationDriver) Init(ctx context.Context, ad keppel.AuthDriver, cfg keppel.Configuration) error {
	osext.MustGetenv("KEPPEL_FEDERATION_REDIS_HOSTNAME") // check config
	opts, err := keppel.GetRedisOptions("KEPPEL_FEDERATION")
	if err != nil {
		return fmt.Errorf("cannot parse federation Redis URL: %s", err.Error())
	}
	d.ownHostname = cfg.APIPublicHostname
	d.prefix = osext.GetenvOrDefault("KEPPEL_FEDERATION_REDIS_PREFIX", "keppel")
	d.rc = redis.NewClient(opts)
	return nil
}

func (d *federationDriver) primaryKey(accountName string) string {
	return fmt.Sprintf("%s-primary-%s", d.prefix, accountName)
}
func (d *federationDriver) replicasKey(accountName string) string {
	return fmt.Sprintf("%s-replicas-%s", d.prefix, accountName)
}
func (d *federationDriver) tokenKey(accountName string) string {
	return fmt.Sprintf("%s-token-%s", d.prefix, accountName)
}

const (
	checkAndClearScript = `
		local v = redis.call('GET', KEYS[1])
		if v == ARGV[1] then
			redis.call('DEL', KEYS[1])
			return 1
		end
		return 0
	`
)

// ClaimAccountName implements the keppel.FederationDriver interface.
func (d *federationDriver) ClaimAccountName(ctx context.Context, account models.Account, subleaseTokenSecret string) (keppel.ClaimResult, error) {
	if account.UpstreamPeerHostName != "" {
		return d.claimReplicaAccount(ctx, account, subleaseTokenSecret)
	}
	return d.claimPrimaryAccount(ctx, account, subleaseTokenSecret)
}

func (d *federationDriver) claimPrimaryAccount(ctx context.Context, account models.Account, subleaseTokenSecret string) (keppel.ClaimResult, error) {
	// defense in depth - the caller should already have verified this
	if subleaseTokenSecret != "" {
		return keppel.ClaimFailed, errors.New("cannot check sublease token when claiming a primary account")
	}

	// three scenarios:
	// 1. no one has a claim -> SETNX will claim it for us, so GET will return our hostname -> success
	// 2. we have a claim -> SETNX does nothing, but GET will return our hostname -> success
	// 3. someone else has a claim -> SETNX does nothing and GET returns their hostname -> error

	key := d.primaryKey(account.Name)
	err := d.rc.SetNX(ctx, key, d.ownHostname, 0).Err()
	if err != nil {
		return keppel.ClaimErrored, err
	}

	primaryHostname, err := d.rc.Get(ctx, key).Result()
	if err != nil {
		return keppel.ClaimErrored, err
	}
	if primaryHostname != d.ownHostname {
		return keppel.ClaimFailed, fmt.Errorf("account name %s is already in use at %s", account.Name, primaryHostname)
	}
	return keppel.ClaimSucceeded, nil
}

func (d *federationDriver) claimReplicaAccount(ctx context.Context, account models.Account, subleaseTokenSecret string) (keppel.ClaimResult, error) {
	// defense in depth - the caller should already have verified this
	if subleaseTokenSecret == "" {
		return keppel.ClaimFailed, errors.New("missing sublease token")
	}

	// validate the sublease token secret
	ok, err := d.rc.Eval(ctx, checkAndClearScript, []string{d.tokenKey(account.Name)}, subleaseTokenSecret).Bool()
	if err != nil {
		return keppel.ClaimErrored, err
	}
	if !ok {
		return keppel.ClaimFailed, errors.New("invalid sublease token (or token was already used)")
	}

	// validate the primary account
	err = d.validatePrimaryHostname(ctx, account, account.UpstreamPeerHostName)
	if err != nil {
		return keppel.ClaimErrored, err
	}

	// all good - add ourselves to the set of replicas
	err = d.rc.SAdd(ctx, d.replicasKey(account.Name), d.ownHostname).Err()
	if err != nil {
		return keppel.ClaimErrored, err
	}
	return keppel.ClaimSucceeded, nil
}

// IssueSubleaseTokenSecret implements the keppel.FederationDriver interface.
func (d *federationDriver) IssueSubleaseTokenSecret(ctx context.Context, account models.Account) (string, error) {
	// defense in depth - the caller should already have verified this
	if account.UpstreamPeerHostName != "" {
		return "", errors.New("operation not allowed for replica accounts")
	}

	// more defense in depth
	err := d.validatePrimaryHostname(ctx, account, d.ownHostname)
	if err != nil {
		return "", err
	}

	// generate a random token with 16 Base64 chars
	tokenBytes := make([]byte, 12)
	_, err = rand.Read(tokenBytes)
	if err != nil {
		return "", fmt.Errorf("could not generate token: %s", err.Error())
	}
	tokenStr := base64.StdEncoding.EncodeToString(tokenBytes)

	// store the random token in Redis
	err = d.rc.Set(ctx, d.tokenKey(account.Name), tokenStr, 0).Err()
	if err != nil {
		return "", fmt.Errorf("could not store token: %s", err.Error())
	}
	return tokenStr, nil
}

// ForfeitAccountName implements the keppel.FederationDriver interface.
func (d *federationDriver) ForfeitAccountName(ctx context.Context, account models.Account) error {
	// case 1: replica account -> just remove ourselves from the set of replicas
	if account.UpstreamPeerHostName != "" {
		return d.rc.SRem(ctx, d.replicasKey(account.Name), d.ownHostname).Err()
	}

	// case 2: primary account -> double-check that we really own it
	err := d.validatePrimaryHostname(ctx, account, d.ownHostname)
	if err != nil {
		return err
	}

	// cannot delete primary account while replicas are still attached to it
	replicaCount, err := d.rc.SCard(ctx, d.replicasKey(account.Name)).Result()
	if err != nil {
		return err
	}
	if replicaCount != 0 {
		return fmt.Errorf("cannot delete primary account %s: %d replicas are still attached to it", account.Name, replicaCount)
	}

	// all validations okay -> cleanup all keys associated with this primary account
	//
	//NOTE: Dynomite does not play well with multi-key DEL commands, so we delete
	// one key at a time
	err = d.rc.Del(ctx, d.tokenKey(account.Name)).Err()
	if err != nil {
		return err
	}
	err = d.rc.Del(ctx, d.replicasKey(account.Name)).Err()
	if err != nil {
		return err
	}
	return d.rc.Del(ctx, d.primaryKey(account.Name)).Err()
}

// RecordExistingAccount implements the keppel.FederationDriver interface.
func (d *federationDriver) RecordExistingAccount(ctx context.Context, account models.Account, now time.Time) error {
	// record this account in Redis using idempotent operations (SETNX for primary, SADD for replica)
	var expectedPrimaryHostname string
	if account.UpstreamPeerHostName == "" {
		expectedPrimaryHostname = d.ownHostname
		err := d.rc.SetNX(ctx, d.primaryKey(account.Name), d.ownHostname, 0).Err()
		if err != nil {
			return err
		}
	} else {
		expectedPrimaryHostname = account.UpstreamPeerHostName
		err := d.rc.SAdd(ctx, d.replicasKey(account.Name), d.ownHostname).Err()
		if err != nil {
			return err
		}
	}

	// check our expectations against the Redis
	return d.validatePrimaryHostname(ctx, account, expectedPrimaryHostname)
}

func (d *federationDriver) validatePrimaryHostname(ctx context.Context, account models.Account, expectedPrimaryHostname string) error {
	// Inconsistencies can arise since we have multiple sources of truth in the
	// Keppels' own database and in the shared Redis. These inconsistencies are
	// incredibly unlikely, however, so making this driver more complicated to
	// better guard against them is a bad tradeoff in my opinion. Instead, we just
	// make sure that the driver loudly complains once it finds an inconsistency,
	// so the operator can take care of fixing it.
	primaryHostname, err := d.rc.Get(ctx, d.primaryKey(account.Name)).Result()
	if errors.Is(err, redis.Nil) {
		primaryHostname = ""
		err = nil
	}
	if err != nil {
		return fmt.Errorf("could not find primary for account %s: %s", account.Name, err.Error())
	}

	if expectedPrimaryHostname != primaryHostname {
		return fmt.Errorf("expected primary for account %s to be hosted by %s, but is actually hosted by %q",
			account.Name, expectedPrimaryHostname, primaryHostname)
	}
	return nil
}

// FindPrimaryAccount implements the keppel.FederationDriver interface.
func (d *federationDriver) FindPrimaryAccount(ctx context.Context, accountName string) (string, error) {
	primaryHostname, err := d.rc.Get(ctx, d.primaryKey(accountName)).Result()
	if errors.Is(err, redis.Nil) {
		return "", keppel.ErrNoSuchPrimaryAccount
	}
	return primaryHostname, err
}
