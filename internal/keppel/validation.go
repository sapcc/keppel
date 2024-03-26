/******************************************************************************
*
*  Copyright 2024 SAP SE
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
func RenderValidationPolicy(account models.Account) *ValidationPolicy {
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
