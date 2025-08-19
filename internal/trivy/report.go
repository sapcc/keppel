// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package trivy

import (
	"encoding/json"
	"fmt"
	"maps"

	. "github.com/majewsky/gg/option"
)

// Report is a type for deserializing a Trivy vulnerability report into.
// We do not import type Report from github.com/aquasecurity/trivy/pkg/types
// because it pulls a cartoonish amount of dependencies into our application that we don't need.
type Report struct {
	// a partially deserialized form of the original report from Trivy
	originalPayload map[string]json.RawMessage

	// specialized deserializations for exactly the fields that we care about
	// (when marshalling, we still use the originalPayload, because these subtypes are not guaranteed to have all relevant fields
	ArtifactName string
	Results      []ReportResult
	Metadata     ReportMetadata

	// fields that we add during processing
	additionalFields map[string]any
}

// UnmarshalReportFromJSON creates a Report object by unmarshalling a report JSON received from Trivy.
//
// NOTE: Use this directly instead of passing the report to json.Unmarshal() to avoid superfluous bytestring copies.
func UnmarshalReportFromJSON(buf []byte) (Report, error) {
	r := Report{
		originalPayload:  make(map[string]json.RawMessage),
		additionalFields: make(map[string]any),
	}
	err := json.Unmarshal(buf, &r.originalPayload)
	if err != nil {
		return Report{}, err
	}

	resultsBuf := r.originalPayload["Results"]
	if len(resultsBuf) > 0 {
		err := json.Unmarshal(resultsBuf, &r.Results)
		if err != nil {
			return Report{}, fmt.Errorf(`while unmarshalling "Results" subsection: %w`, err)
		}
	}

	artifactNameBuf := r.originalPayload["ArtifactName"]
	if len(artifactNameBuf) > 0 {
		err := json.Unmarshal(artifactNameBuf, &r.ArtifactName)
		if err != nil {
			return Report{}, fmt.Errorf(`while unmarshalling "ArtifactName" subsection: %w`, err)
		}
	}

	metadataBuf := r.originalPayload["Metadata"]
	if len(metadataBuf) > 0 {
		err := json.Unmarshal(metadataBuf, &r.Metadata)
		if err != nil {
			return Report{}, fmt.Errorf(`while unmarshalling "Metadata" subsection: %w`, err)
		}
	}

	return r, err
}

// UnmarshalJSON implements the json.Unmarshaler interface.
func (r *Report) UnmarshalJSON(buf []byte) error {
	panic("use trivy.UnmarshalReportFromJSON directly!")
}

// AddField adds an additional top-level field to the serialization of this report.
func (r *Report) AddField(key string, value any) {
	r.additionalFields[key] = value
}

// MarshalJSON implements the json.Marshaler interface.
//
// NOTE: Use this directly instead of passing the report to json.Marshal() to avoid superfluous bytestring copies.
func (r Report) MarshalJSON() ([]byte, error) {
	if len(r.additionalFields) == 0 {
		return json.Marshal(r.originalPayload)
	}

	allFields := maps.Clone(r.additionalFields)
	for k, v := range r.originalPayload {
		_, ok := allFields[k]
		// additionalFields take precedence
		if ok {
			continue
		}

		allFields[k] = v
	}
	return json.Marshal(allFields)
}

// ReportMetadata appears in type Report.
//
// It represents the .Metadata section of a Trivy report,
// but has only exactly those fields that we need.
type ReportMetadata struct {
	OS Option[ReportMetadataOS]
}

// ReportMetadataOS appears in type ReportMetadata.
type ReportMetadataOS struct {
	EndOfSupportLifecycle bool `json:"EOSL"`
	Family                string
	Name                  string
}

// IsRotten returns whether the OS.EndOfSupportLifecycle flag is set.
func (m ReportMetadata) IsRotten() bool {
	return m.OS.IsSomeAnd(func(os ReportMetadataOS) bool { return os.EndOfSupportLifecycle })
}

// ReportResult appears in type Report.
//
// It represents one of the .Results[] sections of a Trivy report,
// but has only exactly those fields that we need.
type ReportResult struct {
	Class           string
	Target          string
	Type            string
	Vulnerabilities []DetectedVulnerability
}

// DetectedVulnerability appears in type ReportResult.
type DetectedVulnerability struct {
	// NOTE: The upstream type is <https://pkg.go.dev/github.com/aquasecurity/trivy/pkg/types#DetectedVulnerability>
	// and inheriting from <https://pkg.go.dev/github.com/aquasecurity/trivy-db/pkg/types#Vulnerability>.
	Description      string
	FixedVersion     string
	LastModifiedDate string
	PublishedDate    string
	Severity         string
	SeveritySource   string
	Status           string
	Title            string
	VulnerabilityID  string
}

// FixIsReleased returns whether v.FixedVersion is non-empty. (This particular
// method name reads better in some situations than `v.FixedVersion != ""`.)
func (v DetectedVulnerability) FixIsReleased() bool {
	return v.FixedVersion != ""
}
