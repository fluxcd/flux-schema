// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package validator

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/fluxcd/flux-schema/api/v1beta1"
)

// NewReportResult converts a Result into its wire-format counterpart.
// Resource is nil when no apiVersion/kind could be recovered (source-load
// or unparsable-YAML failures); the pointer renders as `"resource": null`.
func NewReportResult(r Result) apiv1.ReportResult {
	out := apiv1.ReportResult{
		Source: r.Source,
		Idx:    r.DocIndex,
		Status: r.Status.String(),
		Reason: apiv1.ReportReason(r.Reason),
	}
	if r.APIVersion != "" || r.Kind != "" {
		out.Resource = &apiv1.ReportResource{
			APIVersion: r.APIVersion,
			Kind:       r.Kind,
			Namespace:  r.Namespace,
			Name:       r.Name,
		}
	}
	if len(r.Errors) > 0 {
		out.Violations = make([]apiv1.ReportViolation, len(r.Errors))
		for i, e := range r.Errors {
			out.Violations[i] = apiv1.ReportViolation{Path: e.Path, Message: e.Msg}
		}
	}
	return out
}

// NewReport assembles a Report from the validator outputs. The caller owns
// the reporter string (typically "flux-schema/"+VERSION) and the timestamp
// so tests can pin both.
func NewReport(reporter string, timestamp time.Time, results []Result, summary apiv1.ReportSummary) apiv1.Report {
	body := apiv1.ReportSpec{
		Reporter:  reporter,
		Timestamp: timestamp.UTC().Format(time.RFC3339),
		Summary:   summary,
		Results:   make([]apiv1.ReportResult, len(results)),
	}
	for i, r := range results {
		body.Results[i] = NewReportResult(r)
	}
	return apiv1.Report{
		TypeMeta: metav1.TypeMeta{
			APIVersion: apiv1.GroupVersion.String(),
			Kind:       apiv1.ReportKind,
		},
		Schema: apiv1.ReportSchema,
		Report: body,
	}
}
