// SPDX-FileCopyrightText: 2019 SAP SE
// SPDX-License-Identifier: Apache-2.0

package openstack

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/gophercloud/gophercloud/v2/openstack/identity/v3/domains"
	"github.com/gophercloud/gophercloud/v2/openstack/identity/v3/projects"
	"github.com/sapcc/go-bits/osext"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
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
	keppel.FederationDriverRegistry.Add(func() keppel.FederationDriver { return &federationDriverBasic{} })
}

// PluginTypeID implements the keppel.FederationDriver interface.
func (d *federationDriverBasic) PluginTypeID() string { return "openstack-basic" }

// Init implements the keppel.FederationDriver interface.
func (d *federationDriverBasic) Init(_ context.Context, ad keppel.AuthDriver, cfg keppel.Configuration) error {
	var ok bool
	d.AuthDriver, ok = ad.(*keystoneDriver)
	if !ok {
		return keppel.ErrAuthDriverMismatch
	}

	wlStr := strings.TrimSuffix(osext.MustGetenv("KEPPEL_NAMECLAIM_WHITELIST"), ",")
	for _, wlEntryStr := range strings.Split(wlStr, ",") {
		wlEntryFields := strings.SplitN(wlEntryStr, ":", 2)
		if len(wlEntryFields) != 2 {
			return errors.New(`KEPPEL_NAMECLAIM_WHITELIST must have the form "project1:accountName1,project2:accountName2,..."`)
		}

		projectNameRx, err := regexp.Compile(`^(?:` + wlEntryFields[0] + `)$`)
		if err != nil {
			return err
		}
		accountNameRx, err := regexp.Compile(`^(?:` + wlEntryFields[1] + `)$`)
		if err != nil {
			return err
		}
		d.Whitelist = append(d.Whitelist, nameClaimWhitelistEntry{
			ProjectName: projectNameRx,
			AccountName: accountNameRx,
		})
	}

	return nil
}

// ClaimAccountName implements the keppel.FederationDriver interface.
func (d *federationDriverBasic) ClaimAccountName(ctx context.Context, account models.Account, subleaseTokenSecret string) (keppel.ClaimResult, error) {
	project, err := projects.Get(ctx, d.AuthDriver.IdentityV3, account.AuthTenantID).Extract()
	if err != nil {
		return keppel.ClaimErrored, err
	}
	domain, err := domains.Get(ctx, d.AuthDriver.IdentityV3, project.DomainID).Extract()
	if err != nil {
		return keppel.ClaimErrored, err
	}
	projectName := fmt.Sprintf("%s@%s", project.Name, domain.Name)

	for _, entry := range d.Whitelist {
		projectMatches := entry.ProjectName.MatchString(projectName)
		accountMatches := entry.AccountName.MatchString(string(account.Name))
		if projectMatches && accountMatches {
			return keppel.ClaimSucceeded, nil
		}
	}

	return keppel.ClaimFailed, fmt.Errorf(`account name "%s" is not whitelisted for project "%s"`,
		account.Name, projectName,
	)
}

// IssueSubleaseTokenSecret implements the keppel.FederationDriver interface.
func (d *federationDriverBasic) IssueSubleaseTokenSecret(ctx context.Context, account models.Account) (string, error) {
	return "", nil
}

// ForfeitAccountName implements the keppel.FederationDriver interface.
func (d *federationDriverBasic) ForfeitAccountName(ctx context.Context, account models.Account) error {
	return nil
}

// RecordExistingAccount implements the keppel.FederationDriver interface.
func (d *federationDriverBasic) RecordExistingAccount(ctx context.Context, account models.Account, now time.Time) error {
	return nil
}

// FindPrimaryAccount implements the keppel.FederationDriver interface.
func (d *federationDriverBasic) FindPrimaryAccount(ctx context.Context, accountName models.AccountName) (string, error) {
	return "", keppel.ErrNoSuchPrimaryAccount
}
