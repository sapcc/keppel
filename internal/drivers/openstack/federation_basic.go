/******************************************************************************
*
*  Copyright 2019 SAP SE
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

package openstack

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/gophercloud/gophercloud/openstack/identity/v3/domains"
	"github.com/gophercloud/gophercloud/openstack/identity/v3/projects"
	"github.com/sapcc/go-bits/osext"

	"github.com/sapcc/keppel/internal/keppel"
)

type nameClaimWhitelistEntry struct {
	ProjectName *regexp.Regexp
	AccountName *regexp.Regexp
}

type federationDriverBasic struct {
	AuthDriver *keystoneDriver
	Whitelist  []nameClaimWhitelistEntry
}

func init() {
	keppel.RegisterFederationDriver("openstack-basic", func(ad keppel.AuthDriver, _ keppel.Configuration) (keppel.FederationDriver, error) {
		k, ok := ad.(*keystoneDriver)
		if !ok {
			return nil, keppel.ErrAuthDriverMismatch
		}
		result := &federationDriverBasic{AuthDriver: k}

		wlStr := strings.TrimSuffix(osext.MustGetenv("KEPPEL_NAMECLAIM_WHITELIST"), ",")
		for _, wlEntryStr := range strings.Split(wlStr, ",") {
			wlEntryFields := strings.SplitN(wlEntryStr, ":", 2)
			if len(wlEntryFields) != 2 {
				return nil, errors.New(`KEPPEL_NAMECLAIM_WHITELIST must have the form "project1:accountName1,project2:accountName2,..."`)
			}

			projectNameRx, err := regexp.Compile(`^(?:` + wlEntryFields[0] + `)$`)
			if err != nil {
				return nil, err
			}
			accountNameRx, err := regexp.Compile(`^(?:` + wlEntryFields[1] + `)$`)
			if err != nil {
				return nil, err
			}
			result.Whitelist = append(result.Whitelist, nameClaimWhitelistEntry{
				ProjectName: projectNameRx,
				AccountName: accountNameRx,
			})
		}

		return result, nil
	})
}

//ClaimAccountName implements the keppel.FederationDriver interface.
func (d *federationDriverBasic) ClaimAccountName(account keppel.Account, subleaseTokenSecret string) (keppel.ClaimResult, error) {
	project, err := projects.Get(d.AuthDriver.IdentityV3, account.AuthTenantID).Extract()
	if err != nil {
		return keppel.ClaimErrored, err
	}
	domain, err := domains.Get(d.AuthDriver.IdentityV3, project.DomainID).Extract()
	if err != nil {
		return keppel.ClaimErrored, err
	}
	projectName := fmt.Sprintf("%s@%s", project.Name, domain.Name)

	for _, entry := range d.Whitelist {
		projectMatches := entry.ProjectName.MatchString(projectName)
		accountMatches := entry.AccountName.MatchString(account.Name)
		if projectMatches && accountMatches {
			return keppel.ClaimSucceeded, nil
		}
	}

	return keppel.ClaimFailed, fmt.Errorf(`account name "%s" is not whitelisted for project "%s"`,
		account.Name, projectName,
	)
}

//IssueSubleaseTokenSecret implements the keppel.FederationDriver interface.
func (d *federationDriverBasic) IssueSubleaseTokenSecret(account keppel.Account) (string, error) {
	return "", nil
}

//ForfeitAccountName implements the keppel.FederationDriver interface.
func (d *federationDriverBasic) ForfeitAccountName(account keppel.Account) error {
	return nil
}

//RecordExistingAccount implements the keppel.FederationDriver interface.
func (d *federationDriverBasic) RecordExistingAccount(account keppel.Account, now time.Time) error {
	return nil
}

//FindPrimaryAccount implements the keppel.FederationDriver interface.
func (d *federationDriverBasic) FindPrimaryAccount(accountName string) (string, error) {
	return "", keppel.ErrNoSuchPrimaryAccount
}
