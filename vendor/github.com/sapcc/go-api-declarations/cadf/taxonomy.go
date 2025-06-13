// SPDX-FileCopyrightText: 2022 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

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
