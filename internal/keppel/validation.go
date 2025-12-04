// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package keppel

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/google/cel-go/cel"
	lru "github.com/hashicorp/golang-lru/v2"

	"github.com/sapcc/go-bits/must"

	"github.com/sapcc/keppel/internal/models"
)

// ValidationPolicy represents a validation policy in the API.
type ValidationPolicy struct {
	RequiredLabels  []string `json:"required_labels,omitempty"`
	RuleForManifest string   `json:"rule_for_manifest,omitempty"`
}

// When changing celExpressionRx, also update celLabelExtractionRx
var celExpressionRx = regexp.MustCompile(`^\'([^']+?)\' in labels(\s*&&\s*\'([^']+?)\' in labels)*$`)

// RenderValidationPolicy builds a ValidationPolicy object out of the
// information in the given account model.
func RenderValidationPolicy(account models.ReducedAccount) *ValidationPolicy {
	if account.RuleForManifest == "" {
		return nil
	}

	policy := ValidationPolicy{
		RuleForManifest: account.RuleForManifest,
	}

	// for backwards compatibility, show required_labels field if the CEL expression is identical
	// to the one that we would generate from a required_labels attribute
	if celExpressionRx.MatchString(account.RuleForManifest) {
		policy.RequiredLabels = extractRequiredLabelsFromCEL(account.RuleForManifest)
	}

	return &policy
}

// ApplyToAccount validates this policy and stores it in the given account model.
func (v ValidationPolicy) ApplyToAccount(account *models.Account) *RegistryV2Error {
	// for backwards compatibility, if both validation.rule_for_manifest and validation.required_labels given,
	// accept if and only if the presented CEL expression is the same as what we would generate from the presented list of required labels
	if v.RuleForManifest != "" && v.RequiredLabels != nil {
		generatedRule := generateRuleForManifestFromrequiredLabels(v.RequiredLabels)
		if generatedRule != v.RuleForManifest {
			err := fmt.Errorf(`required labels %q do not match rule for manifest %q`, v.RequiredLabels, v.RuleForManifest)
			return AsRegistryV2Error(err).WithStatus(http.StatusUnprocessableEntity)
		}
	} else if v.RuleForManifest == "" && v.RequiredLabels == nil {
		account.RuleForManifest = v.RuleForManifest
		return nil
	}

	if v.RuleForManifest != "" {
		_, ast, celErr := BuildManifestValidationAST(v.RuleForManifest)
		if celErr != nil {
			err := fmt.Errorf(`invalid CEL expression: %q`, v.RuleForManifest)
			return AsRegistryV2Error(err).WithStatus(http.StatusUnprocessableEntity).WithDetail(celErr.Error())
		}
		if ast.OutputType() != cel.BoolType {
			err := fmt.Errorf(`output of CEL expression must be bool but is %q`, ast.OutputType().TypeName())
			return AsRegistryV2Error(err).WithStatus(http.StatusUnprocessableEntity)
		}
		account.RuleForManifest = v.RuleForManifest
	} else if v.RequiredLabels != nil {
		// for backwards compatibility, if only validation.required_labels given,
		// translate into a CEL expression
		for _, label := range v.RequiredLabels {
			if strings.Contains(label, ",") {
				err := fmt.Errorf(`invalid label name: %q`, label)
				return AsRegistryV2Error(err).WithStatus(http.StatusUnprocessableEntity)
			}
		}
		account.RuleForManifest = generateRuleForManifestFromrequiredLabels(v.RequiredLabels)
	}

	return nil
}

// When changing celLabelExtractionRx, also update celExpressionRx
var celLabelExtractionRx = regexp.MustCompile(`\'([^']+?)\' in labels`)

func extractRequiredLabelsFromCEL(ruleForManifest string) []string {
	matches := celLabelExtractionRx.FindAllStringSubmatch(ruleForManifest, -1)

	var labels []string
	for _, match := range matches {
		if len(match) > 1 {
			labels = append(labels, match[1])
		}
	}

	return labels
}

func generateRuleForManifestFromrequiredLabels(requiredLabels []string) string {
	var propositions []string
	for _, label := range requiredLabels {
		propositions = append(propositions, fmt.Sprintf("'%s' in labels", label))
	}

	return strings.Join(propositions, " && ")
}

var celASTCache = must.Return(lru.New[string, *cel.Ast](128))
var celEnv = must.Return(cel.NewEnv(
	cel.Variable("labels", cel.MapType(cel.StringType, cel.StringType)),
	// TODO: remove DynType and properly declare this
	// https://pkg.go.dev/github.com/google/cel-go@v0.26.1/common/types#NewObjectType looks like a good lead but it is for Protobuf only...
	cel.Variable("layers", cel.ListType(
		cel.MapType(cel.StringType, cel.DynType),
	)),
	cel.Variable("media_type", cel.StringType),
	cel.Variable("repo_name", cel.StringType),
))

// BuildManifestValidationAST produces the abstract syntax tree (AST) for a given CEL expression in the keppel CEL environment.
// If the CEL expression is invalid an error is returned.
func BuildManifestValidationAST(celExpression string) (*cel.Env, *cel.Ast, error) {
	ast, ok := celASTCache.Get(celExpression)
	if !ok {
		var iss *cel.Issues
		ast, iss = celEnv.Compile(celExpression)
		if iss.Err() != nil {
			return nil, nil, iss.Err()
		}
		celASTCache.Add(celExpression, ast)
	}

	return celEnv, ast, nil
}
