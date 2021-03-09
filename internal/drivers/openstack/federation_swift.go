/*******************************************************************************
*
* Copyright 2021 SAP SE
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

package openstack

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"sort"
	"time"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"github.com/gophercloud/utils/openstack/clientconfig"
	"github.com/majewsky/schwift"
	"github.com/majewsky/schwift/gopherschwift"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/keppel/internal/keppel"
)

type federationDriverSwift struct {
	Container   *schwift.Container
	OwnHostName string
}

func init() {
	keppel.RegisterFederationDriver("swift", func(_ keppel.AuthDriver, cfg keppel.Configuration) (keppel.FederationDriver, error) {
		//authenticate service user
		ao, err := clientconfig.AuthOptions(&clientconfig.ClientOpts{
			EnvPrefix: "KEPPEL_FEDERATION_OS_",
		})
		if err != nil {
			return nil, errors.New("cannot find OpenStack credentials for federation driver: " + err.Error())
		}
		ao.AllowReauth = true
		provider, err := createProviderClient(*ao)
		if err != nil {
			return nil, errors.New("cannot connect to OpenStack for federation driver: " + err.Error())
		}

		//find Swift endpoint
		eo := gophercloud.EndpointOpts{
			//note that empty values are acceptable in both fields
			Region:       os.Getenv("KEPPEL_FEDERATION_OS_REGION_NAME"),
			Availability: gophercloud.Availability(os.Getenv("KEPPEL_FEDERATION_OS_INTERFACE")),
		}
		swiftV1, err := openstack.NewObjectStorageV1(provider, eo)
		if err != nil {
			return nil, errors.New("cannot find Swift v1 API for federation driver: " + err.Error())
		}

		//create Swift container if necessary
		swiftAccount, err := gopherschwift.Wrap(swiftV1, &gopherschwift.Options{
			UserAgent: fmt.Sprintf("%s/%s", keppel.Component, keppel.Version),
		})
		if err != nil {
			return nil, err
		}
		container, err := swiftAccount.Container(keppel.MustGetenv("KEPPEL_FEDERATION_SWIFT_CONTAINER")).EnsureExists()
		if err != nil {
			return nil, err
		}

		return &federationDriverSwift{
			Container:   container,
			OwnHostName: cfg.APIPublicURL.Hostname(),
		}, nil
	})
}

type accountFile struct {
	AccountName         string   `json:"-"`
	PrimaryHostName     string   `json:"primary_hostname"`
	ReplicaHostNames    []string `json:"replica_hostnames"`
	SubleaseTokenSecret string   `json:"sublease_token_secret"`
}

func (fd *federationDriverSwift) accountFileObj(accountName string) *schwift.Object {
	return fd.Container.Object(fmt.Sprintf("accounts/%s.json", accountName))
}

//Downloads and parses an account file from the Swift container.
func (fd *federationDriverSwift) readAccountFile(accountName string) (accountFile, error) {
	buf, err := fd.accountFileObj(accountName).Download(nil).AsByteSlice()
	if err != nil {
		if schwift.Is(err, http.StatusNotFound) {
			//account file does not exist -> create an empty one that we can fill now
			return accountFile{AccountName: accountName}, nil
		}
		return accountFile{}, err
	}

	var file accountFile
	err = json.Unmarshal(buf, &file)
	file.AccountName = accountName
	return file, err
}

//Base implementation for all write operations performed by this driver. Swift
//does not have strong consistency, so we reduce the likelihood of accidental
//inconsistencies by performing a write once, then reading the result back
//after a short wait and checking whether our write was persisted.
func (fd *federationDriverSwift) modifyAccountFile(accountName string, modify func(file *accountFile, firstPass bool) error) error {
	fileOld, err := fd.readAccountFile(accountName)
	if err != nil {
		return err
	}

	//check if we are actually changing anything at all (this is a very important
	//optimization for RecordExistingAccount which is a no-op most of the time)
	fileOldModified := fileOld
	err = modify(&fileOldModified, true)
	if err != nil {
		return err
	}
	sort.Strings(fileOldModified.ReplicaHostNames) //to avoid useless inequality
	if reflect.DeepEqual(fileOld, fileOldModified) {
		return nil
	}

	//perform the write
	buf, err := json.Marshal(fileOldModified)
	if err != nil {
		return err
	}
	obj := fd.accountFileObj(accountName)
	logg.Info("federation: writing account file %s", obj.FullName())
	hdr := schwift.NewObjectHeaders()
	hdr.ContentType().Set("application/json")
	err = obj.Upload(bytes.NewReader(buf), nil, hdr.ToOpts())
	if err != nil {
		return err
	}

	//wait a bit, then check if the write was persisted
	time.Sleep(250 * time.Millisecond)
	fileNew, err := fd.readAccountFile(accountName)
	if err != nil {
		return err
	}
	fileNewModified := fileNew
	err = modify(&fileNewModified, false)
	if err != nil {
		return err
	}
	sort.Strings(fileNewModified.ReplicaHostNames) //to avoid useless inequality
	if !reflect.DeepEqual(fileNew, fileNewModified) {
		//^ NOTE: It's tempting to just do `reflect.DeepEqual(fileNew,
		//fildOldModified)` here, but that would be too strict of a condition. We
		//don't care whether someone edited the file right after us, we care
		//whether the contents of our write are still there.
		return fmt.Errorf("write collision while trying to update the account file for %q, please retry", accountName)
	}

	return nil
}

//ClaimAccountName implements the keppel.FederationDriver interface.
func (fd *federationDriverSwift) ClaimAccountName(account keppel.Account, authz keppel.Authorization, subleaseTokenSecret string) (keppel.ClaimResult, error) {
	var (
		isUserError bool
		err         error
	)
	if account.UpstreamPeerHostName != "" {
		isUserError, err = fd.claimReplicaAccount(account, subleaseTokenSecret)
	} else {
		isUserError, err = fd.claimPrimaryAccount(account, subleaseTokenSecret)
	}

	if err != nil {
		if isUserError {
			return keppel.ClaimFailed, err
		}
		return keppel.ClaimErrored, err
	}
	return keppel.ClaimSucceeded, nil
}

func (fd *federationDriverSwift) claimPrimaryAccount(account keppel.Account, subleaseTokenSecret string) (isUserError bool, err error) {
	//defense in depth - the caller should already have verified this
	if subleaseTokenSecret != "" {
		return true, errors.New("cannot check sublease token when claiming a primary account")
	}

	isUserError = false
	err = fd.modifyAccountFile(account.Name, func(file *accountFile, firstPass bool) error {
		if file.PrimaryHostName == "" || file.PrimaryHostName == fd.OwnHostName {
			file.PrimaryHostName = fd.OwnHostName
			return nil
		}
		isUserError = true
		return fmt.Errorf("account name %s is already in use at %s", account.Name, file.PrimaryHostName)
	})
	return isUserError, err
}

func (fd *federationDriverSwift) claimReplicaAccount(account keppel.Account, subleaseTokenSecret string) (isUserError bool, err error) {
	//defense in depth - the caller should already have verified this
	if subleaseTokenSecret == "" {
		return true, errors.New("missing sublease token")
	}

	isUserError = false
	err = fd.modifyAccountFile(account.Name, func(file *accountFile, firstPass bool) error {
		//verify the sublease token only on first pass (in the second pass, it was already cleared)
		if firstPass {
			if file.SubleaseTokenSecret != subleaseTokenSecret {
				isUserError = true
				return errors.New("invalid sublease token (or token was already used)")
			}
			file.SubleaseTokenSecret = ""
		}

		//validate the primary account
		err := fd.verifyAccountOwnership(*file, account.UpstreamPeerHostName)
		if err != nil {
			return err
		}

		//all good - add ourselves to the list of replicas
		file.ReplicaHostNames = addStringToList(file.ReplicaHostNames, fd.OwnHostName)
		return nil
	})
	return isUserError, err
}

//IssueSubleaseTokenSecret implements the keppel.FederationDriver interface.
func (fd *federationDriverSwift) IssueSubleaseTokenSecret(account keppel.Account) (string, error) {
	//generate a random token with 16 Base64 chars
	tokenBytes := make([]byte, 12)
	_, err := rand.Read(tokenBytes)
	if err != nil {
		return "", fmt.Errorf("could not generate token: %s", err.Error())
	}
	tokenStr := base64.StdEncoding.EncodeToString(tokenBytes)

	return tokenStr, fd.modifyAccountFile(account.Name, func(file *accountFile, firstPass bool) error {
		//defense in depth - the caller should already have verified this
		if account.UpstreamPeerHostName != "" {
			return errors.New("operation not allowed for replica accounts")
		}

		//more defense in depth
		err := fd.verifyAccountOwnership(*file, fd.OwnHostName)
		if err != nil {
			return err
		}

		file.SubleaseTokenSecret = tokenStr
		return nil
	})
}

//ForfeitAccountName implements the keppel.FederationDriver interface.
func (fd *federationDriverSwift) ForfeitAccountName(account keppel.Account) error {
	//case 1: replica account -> just remove ourselves from the set of replicas
	if account.UpstreamPeerHostName != "" {
		return fd.modifyAccountFile(account.Name, func(file *accountFile, _ bool) error {
			file.ReplicaHostNames = removeStringFromList(file.ReplicaHostNames, fd.OwnHostName)
			return nil
		})
	}

	//case 2: primary account -> perform sanity checks, then delete entire account file
	file, err := fd.readAccountFile(account.Name)
	if err != nil {
		return err
	}
	err = fd.verifyAccountOwnership(file, fd.OwnHostName)
	if err != nil {
		return err
	}
	if len(file.ReplicaHostNames) > 0 {
		return fmt.Errorf("cannot delete primary account %s: %d replicas are still attached to it", account.Name, len(file.ReplicaHostNames))
	}
	return fd.accountFileObj(account.Name).Delete(nil, nil)
}

//RecordExistingAccount implements the keppel.FederationDriver interface.
func (fd *federationDriverSwift) RecordExistingAccount(account keppel.Account, now time.Time) error {
	//Inconsistencies can arise since we have multiple sources of truth in the
	//Keppels' own database and in the shared Swift container. These
	//inconsistencies are incredibly unlikely, however, so making this driver
	//more complicated to better guard against them is a bad tradeoff in my
	//opinion. Instead, we just make sure that the driver loudly complains once
	//it finds an inconsistency, so the operator can take care of fixing it.
	return fd.modifyAccountFile(account.Name, func(file *accountFile, _ bool) error {
		//check that the primary hostname is correct, or fill in if missing
		var expectedPrimaryHostName string
		if account.UpstreamPeerHostName == "" {
			expectedPrimaryHostName = fd.OwnHostName
		} else {
			expectedPrimaryHostName = account.UpstreamPeerHostName
		}
		switch file.PrimaryHostName {
		case "", expectedPrimaryHostName:
			file.PrimaryHostName = expectedPrimaryHostName
		default:
			return fmt.Errorf("expected primary for account %s to be hosted by %s, but is actually hosted by %q",
				account.Name, expectedPrimaryHostName, file.PrimaryHostName)
		}

		//if we are a replica, make sure our name is entered in the ReplicaHostNames
		if account.UpstreamPeerHostName != "" {
			file.ReplicaHostNames = addStringToList(file.ReplicaHostNames, fd.OwnHostName)
		}

		return nil
	})
}

func (fd *federationDriverSwift) verifyAccountOwnership(file accountFile, expectedPrimaryHostName string) error {
	if file.PrimaryHostName != expectedPrimaryHostName {
		return fmt.Errorf("expected primary for account %s to be hosted by %s, but is actually hosted by %q",
			file.AccountName, expectedPrimaryHostName, file.PrimaryHostName)
	}
	return nil
}

//FindPrimaryAccount implements the keppel.FederationDriver interface.
func (fd *federationDriverSwift) FindPrimaryAccount(accountName string) (peerHostName string, err error) {
	file, err := fd.readAccountFile(accountName)
	if err != nil {
		return "", err
	}
	if file.PrimaryHostName == "" {
		return "", keppel.ErrNoSuchPrimaryAccount
	}
	return file.PrimaryHostName, nil
}

func addStringToList(list []string, value string) []string {
	for _, elem := range list {
		if elem == value {
			return list
		}
	}
	return append(list, value)
}

func removeStringFromList(list []string, value string) []string {
	result := make([]string, 0, len(list))
	for _, elem := range list {
		if elem != value {
			result = append(result, elem)
		}
	}
	return result
}
