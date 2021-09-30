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
	"testing"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/test"
	"gopkg.in/gorp.v2"
)

var (
	//only for use with .MustUpload()
	fooRepoRef = keppel.Repository{AccountName: "test1", Name: "foo"}
)

func setup(t *testing.T) (*Janitor, test.Setup) {
	s := test.NewSetup(t,
		test.WithPeerAPI,
		test.WithAccount(keppel.Account{Name: "test1", AuthTenantID: "test1authtenant"}),
		test.WithRepo(keppel.Repository{AccountName: "test1", Name: "foo"}),
		test.WithQuotas,
	)
	j := NewJanitor(s.Config, s.FD, s.SD, s.ICD, s.DB, s.Auditor).OverrideTimeNow(s.Clock.Now).OverrideGenerateStorageID(s.SIDGenerator.Next)
	return j, s
}

func forAllReplicaTypes(t *testing.T, action func(string)) {
	action("on_first_use")
	action("from_external_on_first_use")
}

func setupReplica(t *testing.T, s1 test.Setup, strategy string) (*Janitor, test.Setup) {
	testAccount := keppel.Account{
		Name:         "test1",
		AuthTenantID: "test1authtenant",
	}
	switch strategy {
	case "on_first_use":
		testAccount.UpstreamPeerHostName = "registry.example.org"
	case "from_external_on_first_use":
		testAccount.ExternalPeerURL = "registry.example.org/test1"
		testAccount.ExternalPeerUserName = "replication@registry-secondary.example.org"
		testAccount.ExternalPeerPassword = test.ReplicationPassword
	default:
		t.Fatalf("unknown strategy: %q", strategy)
	}

	s := test.NewSetup(t,
		test.IsSecondaryTo(&s1),
		test.WithPeerAPI,
		test.WithAccount(testAccount),
		test.WithRepo(keppel.Repository{AccountName: "test1", Name: "foo"}),
		test.WithQuotas,
	)

	j2 := NewJanitor(s.Config, s.FD, s.SD, s.ICD, s.DB, s.Auditor).OverrideTimeNow(s.Clock.Now).OverrideGenerateStorageID(s.SIDGenerator.Next)
	return j2, s
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err.Error())
	}
}

func mustExec(t *testing.T, db gorp.SqlExecutor, query string, args ...interface{}) {
	t.Helper()
	_, err := db.Exec(query, args...)
	if err != nil {
		t.Fatal(err.Error())
	}
}

func expectSuccess(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Error("expected err = nil, but got: " + err.Error())
	}
}

func expectError(t *testing.T, expected string, actual error) {
	t.Helper()
	if actual == nil {
		t.Errorf("expected err = %q, but got <nil>", expected)
	} else if expected != actual.Error() {
		t.Errorf("expected err = %q, but got %q", expected, actual.Error())
	}
}
