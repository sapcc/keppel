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
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/go-redis/redis"

	"github.com/sapcc/keppel/internal/keppel"
)

type federationDriver struct {
	ownHostname string
	prefix      string
	rc          *redis.Client
}

func init() {
	keppel.RegisterFederationDriver("redis", func(_ keppel.AuthDriver, cfg keppel.Configuration) (keppel.FederationDriver, error) {
		prefix := os.Getenv("KEPPEL_FEDERATION_REDIS_PREFIX")
		if prefix == "" {
			prefix = "keppel"
		}
		keppel.MustGetenv("KEPPEL_FEDERATION_REDIS_HOSTNAME") // check config
		opts, err := keppel.GetRedisOptions("KEPPEL_FEDERATION")
		if err != nil {
			return nil, fmt.Errorf("cannot parse federation Redis URL: %s", err.Error())
		}
		return &federationDriver{
			ownHostname: cfg.APIPublicHostname,
			prefix:      prefix,
			rc:          redis.NewClient(opts),
		}, nil
	})
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

//ClaimAccountName implements the keppel.FederationDriver interface.
func (d *federationDriver) ClaimAccountName(account keppel.Account, subleaseTokenSecret string) (keppel.ClaimResult, error) {
	if account.UpstreamPeerHostName != "" {
		return d.claimReplicaAccount(account, subleaseTokenSecret)
	}
	return d.claimPrimaryAccount(account, subleaseTokenSecret)
}

func (d *federationDriver) claimPrimaryAccount(account keppel.Account, subleaseTokenSecret string) (keppel.ClaimResult, error) {
	//defense in depth - the caller should already have verified this
	if subleaseTokenSecret != "" {
		return keppel.ClaimFailed, errors.New("cannot check sublease token when claiming a primary account")
	}

	//three scenarios:
	//1. no one has a claim -> SETNX will claim it for us, so GET will return our hostname -> success
	//2. we have a claim -> SETNX does nothing, but GET will return our hostname -> success
	//3. someone else has a claim -> SETNX does nothing and GET returns their hostname -> error

	key := d.primaryKey(account.Name)
	err := d.rc.SetNX(key, d.ownHostname, 0).Err()
	if err != nil {
		return keppel.ClaimErrored, err
	}

	primaryHostname, err := d.rc.Get(key).Result()
	if err != nil {
		return keppel.ClaimErrored, err
	}
	if primaryHostname != d.ownHostname {
		return keppel.ClaimFailed, fmt.Errorf("account name %s is already in use at %s", account.Name, primaryHostname)
	}
	return keppel.ClaimSucceeded, nil
}

func (d *federationDriver) claimReplicaAccount(account keppel.Account, subleaseTokenSecret string) (keppel.ClaimResult, error) {
	//defense in depth - the caller should already have verified this
	if subleaseTokenSecret == "" {
		return keppel.ClaimFailed, errors.New("missing sublease token")
	}

	//validate the sublease token secret
	ok, err := d.rc.Eval(checkAndClearScript, []string{d.tokenKey(account.Name)}, subleaseTokenSecret).Bool()
	if err != nil {
		return keppel.ClaimErrored, err
	}
	if !ok {
		return keppel.ClaimFailed, errors.New("invalid sublease token (or token was already used)")
	}

	//validate the primary account
	err = d.validatePrimaryHostname(account, account.UpstreamPeerHostName)
	if err != nil {
		return keppel.ClaimErrored, err
	}

	//all good - add ourselves to the set of replicas
	err = d.rc.SAdd(d.replicasKey(account.Name), d.ownHostname).Err()
	if err != nil {
		return keppel.ClaimErrored, err
	}
	return keppel.ClaimSucceeded, nil
}

//IssueSubleaseTokenSecret implements the keppel.FederationDriver interface.
func (d *federationDriver) IssueSubleaseTokenSecret(account keppel.Account) (string, error) {
	//defense in depth - the caller should already have verified this
	if account.UpstreamPeerHostName != "" {
		return "", errors.New("operation not allowed for replica accounts")
	}

	//more defense in depth
	err := d.validatePrimaryHostname(account, d.ownHostname)
	if err != nil {
		return "", err
	}

	//generate a random token with 16 Base64 chars
	tokenBytes := make([]byte, 12)
	_, err = rand.Read(tokenBytes)
	if err != nil {
		return "", fmt.Errorf("could not generate token: %s", err.Error())
	}
	tokenStr := base64.StdEncoding.EncodeToString(tokenBytes)

	//store the random token in Redis
	err = d.rc.Set(d.tokenKey(account.Name), tokenStr, 0).Err()
	if err != nil {
		return "", fmt.Errorf("could not store token: %s", err.Error())
	}
	return tokenStr, nil
}

//ForfeitAccountName implements the keppel.FederationDriver interface.
func (d *federationDriver) ForfeitAccountName(account keppel.Account) error {
	//case 1: replica account -> just remove ourselves from the set of replicas
	if account.UpstreamPeerHostName != "" {
		return d.rc.SRem(d.replicasKey(account.Name), d.ownHostname).Err()
	}

	//case 2: primary account -> double-check that we really own it
	err := d.validatePrimaryHostname(account, d.ownHostname)
	if err != nil {
		return err
	}

	//cannot delete primary account while replicas are still attached to it
	replicaCount, err := d.rc.SCard(d.replicasKey(account.Name)).Result()
	if err != nil {
		return err
	}
	if replicaCount != 0 {
		return fmt.Errorf("cannot delete primary account %s: %d replicas are still attached to it", account.Name, replicaCount)
	}

	//all validations okay -> cleanup all keys associated with this primary account
	//
	//NOTE: Dynomite does not play well with multi-key DEL commands, so we delete
	//one key at a time
	err = d.rc.Del(d.tokenKey(account.Name)).Err()
	if err != nil {
		return err
	}
	err = d.rc.Del(d.replicasKey(account.Name)).Err()
	if err != nil {
		return err
	}
	return d.rc.Del(d.primaryKey(account.Name)).Err()
}

//RecordExistingAccount implements the keppel.FederationDriver interface.
func (d *federationDriver) RecordExistingAccount(account keppel.Account, now time.Time) error {
	//record this account in Redis using idempotent operations (SETNX for primary, SADD for replica)
	var expectedPrimaryHostname string
	if account.UpstreamPeerHostName == "" {
		expectedPrimaryHostname = d.ownHostname
		err := d.rc.SetNX(d.primaryKey(account.Name), d.ownHostname, 0).Err()
		if err != nil {
			return err
		}
	} else {
		expectedPrimaryHostname = account.UpstreamPeerHostName
		err := d.rc.SAdd(d.replicasKey(account.Name), d.ownHostname).Err()
		if err != nil {
			return err
		}
	}

	//check our expectations against the Redis
	return d.validatePrimaryHostname(account, expectedPrimaryHostname)
}

func (d *federationDriver) validatePrimaryHostname(account keppel.Account, expectedPrimaryHostname string) error {
	//Inconsistencies can arise since we have multiple sources of truth in the
	//Keppels' own database and in the shared Redis. These inconsistencies are
	//incredibly unlikely, however, so making this driver more complicated to
	//better guard against them is a bad tradeoff in my opinion. Instead, we just
	//make sure that the driver loudly complains once it finds an inconsistency,
	//so the operator can take care of fixing it.
	primaryHostname, err := d.rc.Get(d.primaryKey(account.Name)).Result()
	if err == redis.Nil {
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

//FindPrimaryAccount implements the keppel.FederationDriver interface.
func (d *federationDriver) FindPrimaryAccount(accountName string) (string, error) {
	primaryHostname, err := d.rc.Get(d.primaryKey(accountName)).Result()
	if err == redis.Nil {
		return "", keppel.ErrNoSuchPrimaryAccount
	}
	return primaryHostname, err
}
