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

	"github.com/gophercloud/gophercloud/openstack/identity/v3/domains"
	"github.com/gophercloud/gophercloud/openstack/identity/v3/projects"
	"github.com/sapcc/keppel/internal/keppel"
)

type nameClaimWhitelistEntry struct {
	ProjectName *regexp.Regexp
	AccountName *regexp.Regexp
}

type nameClaimDriverBasic struct {
	AuthDriver *keystoneDriver
	Whitelist  []nameClaimWhitelistEntry
}

func init() {
	keppel.RegisterNameClaimDriver("keystone", func(ad keppel.AuthDriver, _ keppel.Configuration) (keppel.NameClaimDriver, error) {
		k, ok := ad.(*keystoneDriver)
		if !ok {
			return nil, keppel.ErrAuthDriverMismatch
		}
		result := &nameClaimDriverBasic{AuthDriver: k}

		wlStr := strings.TrimSuffix(mustGetenv("KEPPEL_NAMECLAIM_WHITELIST"), ",")
		for _, wlEntryStr := range strings.Split(wlStr, ",") {
			wlEntryFields := strings.SplitN(wlEntryStr, ":", 2)
			if len(wlEntryFields) != 2 {
				return nil, errors.New(`KEPPEL_NAMECLAIM_WHITELIST must have the form "project1:accountName1,project2:accountName2,..."`)
			}

			projectNameRx, err := regexp.Compile(`^` + wlEntryFields[0] + `$`)
			if err != nil {
				return nil, err
			}
			accountNameRx, err := regexp.Compile(`^` + wlEntryFields[1] + `$`)
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

//Check implements the keppel.NameClaimDriver interface.
func (d *nameClaimDriverBasic) Check(claim keppel.NameClaim) error {
	project, err := projects.Get(d.AuthDriver.IdentityV3, claim.AuthTenantID).Extract()
	if err != nil {
		return err
	}
	domain, err := domains.Get(d.AuthDriver.IdentityV3, project.DomainID).Extract()
	if err != nil {
		return err
	}
	projectName := fmt.Sprintf("%s@%s", project.Name, domain.Name)

	for _, entry := range d.Whitelist {
		projectMatches := entry.ProjectName.MatchString(projectName)
		accountMatches := entry.AccountName.MatchString(claim.AccountName)
		if projectMatches && accountMatches {
			return nil
		}
	}

	return fmt.Errorf(`account name "%s" is not whitelisted for project "%s"`,
		claim.AccountName, projectName,
	)
}

//Commit implements the keppel.NameClaimDriver interface.
func (d *nameClaimDriverBasic) Commit(claim keppel.NameClaim) error {
	return d.Check(claim)
}
