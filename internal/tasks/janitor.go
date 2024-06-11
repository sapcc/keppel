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
	"encoding/json"
	"errors"
	"math/rand"
	"net/http"
	"net/url"
	"time"

	"github.com/sapcc/go-api-declarations/cadf"
	"github.com/sapcc/go-bits/audittools"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/processor"
)

func init() {
	keppel.UserIdentityRegistry.Add(func() keppel.UserIdentity {
		return keppel.UserIdentity(janitorUserIdentity{})
	})
}

// janitorDummyRequest can be put in the Request field of type keppel.AuditContext.
var janitorDummyRequest = &http.Request{URL: &url.URL{
	Scheme: "http",
	Host:   "localhost",
	Path:   "keppel-janitor",
}}

// Janitor contains the toolbox of the keppel-janitor process.
type Janitor struct {
	cfg     keppel.Configuration
	fd      keppel.FederationDriver
	sd      keppel.StorageDriver
	icd     keppel.InboundCacheDriver
	db      *keppel.DB
	auditor keppel.Auditor

	// non-pure functions that can be replaced by deterministic doubles for unit tests
	timeNow           func() time.Time
	generateStorageID func() string
	addJitter         func(time.Duration) time.Duration
}

// NewJanitor creates a new Janitor.
func NewJanitor(cfg keppel.Configuration, fd keppel.FederationDriver, sd keppel.StorageDriver, icd keppel.InboundCacheDriver, db *keppel.DB, auditor keppel.Auditor) *Janitor {
	j := &Janitor{cfg, fd, sd, icd, db, auditor, time.Now, keppel.GenerateStorageID, addJitter}
	return j
}

// OverrideTimeNow replaces time.Now with a test double.
func (j *Janitor) OverrideTimeNow(timeNow func() time.Time) *Janitor {
	j.timeNow = timeNow
	return j
}

// OverrideGenerateStorageID replaces keppel.GenerateStorageID with a test double.
func (j *Janitor) OverrideGenerateStorageID(generateStorageID func() string) *Janitor {
	j.generateStorageID = generateStorageID
	return j
}

// DisableJitter replaces addJitter with a no-op for this Janitor.
func (j *Janitor) DisableJitter() {
	j.addJitter = func(d time.Duration) time.Duration { return d }
}

// addJitter returns a random duration within +/- 10% of the requested value.
// This can be used to even out the load on a scheduled job over time, by
// spreading jobs that would normally be scheduled right next to each other out
// over time without corrupting the individual schedules too much.
func addJitter(duration time.Duration) time.Duration {
	//nolint:gosec // This is not crypto-relevant, so math/rand is okay.
	r := rand.Float64() //NOTE: 0 <= r < 1
	return time.Duration(float64(duration) * (0.9 + 0.2*r))
}

func (j *Janitor) processor() *processor.Processor {
	return processor.New(j.cfg, j.db, j.sd, j.icd, j.auditor, j.fd).OverrideTimeNow(j.timeNow).OverrideGenerateStorageID(j.generateStorageID)
}

////////////////////////////////////////////////////////////////////////////////
// janitorUserIdentity

// janitorUserIdentity is a keppel.UserIdentity for the janitor user. It is only
// used for generating audit events and issuing tokens for internal services like Trivy.
type janitorUserIdentity struct {
	TaskName string
	GCPolicy *keppel.GCPolicy
}

// PluginTypeID implements the keppel.UserIdentity interface.
func (uid janitorUserIdentity) PluginTypeID() string {
	return "janitor"
}

// HasPermission implements the keppel.UserIdentity interface.
func (uid janitorUserIdentity) HasPermission(perm keppel.Permission, tenantID string) bool {
	return false
}

// UserType implements the keppel.UserIdentity interface.
func (uid janitorUserIdentity) UserType() keppel.UserType {
	return keppel.JanitorUser
}

// UserName implements the keppel.UserIdentity interface.
func (uid janitorUserIdentity) UserName() string {
	return ""
}

// UserInfo implements the keppel.UserIdentity interface.
func (uid janitorUserIdentity) UserInfo() audittools.UserInfo {
	return janitorUserInfo(uid)
}

// SerializeToJSON implements the keppel.UserIdentity interface.
func (uid janitorUserIdentity) SerializeToJSON() (payload []byte, err error) {
	if uid.GCPolicy != nil {
		return nil, errors.New("janitorUserIdentity.SerializeToJSON is not allowed")
	}
	return json.Marshal(uid.TaskName)
}

// DeserializeFromJSON implements the keppel.UserIdentity interface.
func (uid janitorUserIdentity) DeserializeFromJSON(in []byte, ad keppel.AuthDriver) error {
	return json.Unmarshal(in, &uid.TaskName)
}

////////////////////////////////////////////////////////////////////////////////
// janitorUserInfo

// janitorUserInfo is an audittools.NonStandardUserInfo representing the
// keppel-janitor (who does not have a corresponding OpenStack user). It can be
// used via `type JanitorUserIdentity`.
type janitorUserInfo struct {
	TaskName string
	GCPolicy *keppel.GCPolicy
}

// UserUUID implements the audittools.UserInfo interface.
func (janitorUserInfo) UserUUID() string {
	return "" // unused
}

// UserName implements the audittools.UserInfo interface.
func (janitorUserInfo) UserName() string {
	return "" // unused
}

// UserDomainName implements the audittools.UserInfo interface.
func (janitorUserInfo) UserDomainName() string {
	return "" // unused
}

// ProjectScopeUUID implements the audittools.UserInfo interface.
func (janitorUserInfo) ProjectScopeUUID() string {
	return "" // unused
}

// ProjectScopeName implements the audittools.UserInfo interface.
func (janitorUserInfo) ProjectScopeName() string {
	return "" // unused
}

// ProjectScopeDomainName implements the audittools.UserInfo interface.
func (janitorUserInfo) ProjectScopeDomainName() string {
	return "" // unused
}

// DomainScopeUUID implements the audittools.UserInfo interface.
func (janitorUserInfo) DomainScopeUUID() string {
	return "" // unused
}

// DomainScopeName implements the audittools.UserInfo interface.
func (janitorUserInfo) DomainScopeName() string {
	return "" // unused
}

// ApplicationCredentialID implements the audittools.UserInfo interface.
func (janitorUserInfo) ApplicationCredentialID() string {
	return "" // unused
}

// AsInitiator implements the audittools.NonStandardUserInfo interface.
func (u janitorUserInfo) AsInitiator() cadf.Resource {
	res := cadf.Resource{
		TypeURI: "service/docker-registry/janitor-task",
		Name:    u.TaskName,
		Domain:  "keppel",
		ID:      u.TaskName,
	}
	if u.GCPolicy != nil {
		gcPolicyJSON, _ := json.Marshal(*u.GCPolicy)
		res.Attachments = append(res.Attachments, cadf.Attachment{
			Name:    "gc-policy",
			TypeURI: "mime:application/json",
			Content: string(gcPolicyJSON),
		})
	}
	return res
}
