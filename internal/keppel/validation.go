// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package keppel

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/sapcc/keppel/internal/models"
)

// ValidationPolicy represents a validation policy in the API.
type ValidationPolicy struct {
	RequiredLabels []string `json:"required_labels,omitempty"`
}

// RenderValidationPolicy builds a ValidationPolicy object out of the
// information in the given account model.
func RenderValidationPolicy(account models.ReducedAccount) *ValidationPolicy {
	if account.RequiredLabels == "" {
		return nil
	}

	return &ValidationPolicy{
		RequiredLabels: account.SplitRequiredLabels(),
	}
}

// ApplyToAccount validates this policy and stores it in the given account model.
func (v ValidationPolicy) ApplyToAccount(account *models.Account) *RegistryV2Error {
	for _, label := range v.RequiredLabels {
		if strings.Contains(label, ",") {
			err := fmt.Errorf(`invalid label name: %q`, label)
			return AsRegistryV2Error(err).WithStatus(http.StatusUnprocessableEntity)
		}
	}

	account.RequiredLabels = strings.Join(v.RequiredLabels, ",")
	return nil
}
