// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package junitxml

import (
	"encoding/xml"
	"io"
	"strings"

	apiv1 "github.com/fluxcd/flux-schema/api/v1beta1"
)

type TestSuites struct {
	XMLName  xml.Name    `xml:"testsuites"`
	Name     string      `xml:"name,attr,omitempty"`
	Tests    int         `xml:"tests,attr"`
	Failures int         `xml:"failures,attr,omitempty"`
	Skipped  int         `xml:"skipped,attr,omitempty"`
	Suites   []TestSuite `xml:"testsuite"`
}

func (t TestSuites) Write(w io.Writer) error {
	if _, err := io.WriteString(w, xml.Header); err != nil {
		return err
	}

	enc := xml.NewEncoder(w)
	defer enc.Close()

	enc.Indent("", "    ")

	if err := enc.Encode(t); err != nil {
		return err
	}

	_, err := io.WriteString(w, "\n")
	return err
}

type TestSuite struct {
	Name      string     `xml:"name,attr"`
	Tests     int        `xml:"tests,attr"`
	Failures  int        `xml:"failures,attr,omitempty"`
	Skipped   int        `xml:"skipped,attr,omitempty"`
	TestCases []TestCase `xml:"testcase"`
}

type TestCase struct {
	Classname string   `xml:"classname,attr"`
	Name      string   `xml:"name,attr"`
	File      string   `xml:"file,attr,omitempty"`
	Time      float64  `xml:"time,attr,omitempty"`
	Failure   *Failure `xml:"failure,omitempty"`
	Skipped   *Skipped `xml:"skipped,omitempty"`
}

type Failure struct {
	Message string `xml:"message,attr,omitempty"`
	Type    string `xml:"type,attr,omitempty"`
	Body    string `xml:",chardata"`
}

type Skipped struct {
	Message string `xml:",chardata"`
}

func FromReport(report apiv1.Report) TestSuites {
	spec := report.Report

	suite := TestSuite{
		Name:      spec.Reporter,
		Tests:     spec.Summary.Total,
		Failures:  spec.Summary.Invalid,
		Skipped:   spec.Summary.Skipped,
		TestCases: make([]TestCase, 0, len(spec.Results)),
	}

	for _, result := range spec.Results {
		suite.TestCases = append(suite.TestCases, testCaseFromResult(result))
	}

	return TestSuites{
		Name:     "flux-schema",
		Tests:    spec.Summary.Total,
		Failures: spec.Summary.Invalid,
		Skipped:  spec.Summary.Skipped,
		Suites:   []TestSuite{suite},
	}
}

func testCaseFromResult(result apiv1.ReportResult) TestCase {
	tc := TestCase{
		Classname: result.Source,
		Name:      result.Source,
		File:      result.Source,
	}
	if result.Origin != "" {
		tc.File = result.Origin
	}

	if result.Resource != nil {
		tc.Classname = result.Resource.APIVersion + "/" + result.Resource.Kind
		tc.Name = resourceName(*result.Resource)
	}

	switch result.Status {
	case "invalid":
		tc.Failure = &Failure{
			// TODO: Human readable message
			Message: string(result.Reason),
			Type:    string(result.Reason),
			Body:    violationMessage(result.Violations),
		}
	case "skipped":
		tc.Skipped = &Skipped{
			// TODO: Human readable message
			Message: string(result.Reason),
		}
	}

	return tc
}

func resourceName(resource apiv1.ReportResource) string {
	if resource.Namespace != "" {
		return resource.Namespace + "/" + resource.Name
	}
	return resource.Name
}

func violationMessage(violations []apiv1.ReportViolation) string {
	var b strings.Builder

	for i, violation := range violations {
		if i > 0 {
			b.WriteByte('\n')
		}

		if violation.Path != "" {
			b.WriteString(violation.Path)
			b.WriteString(": ")
		}

		b.WriteString(violation.Message)
	}

	return b.String()
}
