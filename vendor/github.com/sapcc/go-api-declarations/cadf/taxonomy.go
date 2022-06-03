/*******************************************************************************
*
* Copyright 2022 SAP SE
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

package cadf

import (
	"net/http"
	"strings"
)

// Action enumerates some of the valid values for CADF actions.
// Note that this list is not complete and there are other action types that are also valid.
type Action string

const (
	BackupAction       Action = "backup"
	CaptureAction      Action = "capture"
	CreateAction       Action = "create"
	ConfigureAction    Action = "configure"
	ReadAction         Action = "read"
	ListAction         Action = "list"
	UpdateAction       Action = "update"
	DeleteAction       Action = "delete"
	MonitorAction      Action = "monitor"
	StartAction        Action = "start"
	StopAction         Action = "stop"
	DeployAction       Action = "deploy"
	UndeployAction     Action = "undeploy"
	EnableAction       Action = "enable"
	DisableAction      Action = "disable"
	SendAction         Action = "send"
	ReceiveAction      Action = "receive"
	AuthenticateAction Action = "authenticate"
	LoginAction        Action = "authenticate/login"
	RevokeAction       Action = "revoke"
	RenewAction        Action = "renew"
	RestoreAction      Action = "restore"
	EvaluateAction     Action = "evaluate"
	AllowAction        Action = "allow"
	DenyAction         Action = "deny"
	NotifyAction       Action = "notify"
	UnknownAction      Action = "unknown"
)

// Outcome enumerates valid values for CADF outcomes.
type Outcome string

const (
	SuccessOutcome Outcome = "success"
	FailureOutcome Outcome = "failure"
	PendingOutcome Outcome = "pending"
)

// GetAction returns the corresponding Action for a HTTP request method.
func GetAction(method string) Action {
	switch strings.ToUpper(method) {
	case http.MethodGet:
		return ReadAction
	case http.MethodHead:
		return ReadAction
	case http.MethodPost:
		return CreateAction
	case http.MethodPut:
		return UpdateAction
	case http.MethodDelete:
		return DeleteAction
	case http.MethodPatch:
		return UpdateAction
	case http.MethodOptions:
		return ReadAction
	default:
		return UnknownAction
	}
}
