// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

// This file contains string-format validators ported from
// k8s.io/kube-openapi/pkg/validation/strfmt
// (Apache-2.0, Copyright 2015 go-swagger maintainers).
// The ports preserve the upstream accept/reject
// sets so that flux-schema matches kube-apiserver's CRD admission behavior
// for the formats it covers.
//
// Upstream references (kube-openapi commit 9bd5c66d9911):
//   - duration.go — ParseDuration, durationMatcher, timeUnits, timeMultiplier
//   - date.go     — IsDate, RFC3339FullDate
//   - time.go     — IsDateTime, DateTimePattern, rxDateTime

package validator

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

const rfc3339FullDate = "2006-01-02"

// dateTimePattern mirrors kube-openapi strfmt.DateTimePattern.
const dateTimePattern = `^([0-9]{2}):([0-9]{2}):([0-9]{2})(.[0-9]+)?(z|([+-][0-9]{2}:[0-9]{2}))$`

var (
	durationMatcher = regexp.MustCompile(`((\d+)\s*([A-Za-zµ]+))`)
	rxDateTime      = regexp.MustCompile(dateTimePattern)

	// timeUnits mirrors kube-openapi strfmt.timeUnits. We only need the
	// unit aliases to decide "is this a valid duration?" — we don't need
	// the multiplier table, because we don't care what the duration is,
	// only that it parses.
	timeUnits = [][]string{
		{"ns", "nano"},
		{"us", "µs", "micro"},
		{"ms", "milli"},
		{"s", "sec"},
		{"m", "min"},
		{"h", "hr", "hour"},
		{"d", "day"},
		{"w", "wk", "week"},
	}
)

// registerKubernetesFormats installs format validators whose accept/reject
// semantics match kube-apiserver. Called once from New().
//
// jsonschema/v6 disables format assertions by default for draft/2020-12;
// we enable them via AssertFormat so that format violations become errors
// rather than annotations.
func registerKubernetesFormats(c *jsonschema.Compiler) {
	c.AssertFormat()

	duration := &jsonschema.Format{Name: "duration", Validate: validateDuration}
	c.RegisterFormat(duration)

	date := &jsonschema.Format{Name: "date", Validate: validateDate}
	c.RegisterFormat(date)

	// kube-openapi registers IsDateTime under "datetime" (no hyphen); we
	// register the same handler under both spellings so schemas that use
	// either form behave identically.
	c.RegisterFormat(&jsonschema.Format{Name: "datetime", Validate: validateDateTime})
	c.RegisterFormat(&jsonschema.Format{Name: "date-time", Validate: validateDateTime})

	// kube-apiserver does not register a "time" format; we install a
	// permissive no-op so schemas declaring format: time do not reject
	// values that kube-apiserver would accept.
	c.RegisterFormat(&jsonschema.Format{Name: "time", Validate: validateTime})
}

// validateDuration is a port of kube-openapi strfmt.IsDuration / ParseDuration.
func validateDuration(v any) error {
	s, ok := v.(string)
	if !ok {
		return nil
	}
	if _, err := time.ParseDuration(s); err == nil {
		return nil
	}

	matched := false
	for _, match := range durationMatcher.FindAllStringSubmatch(s, -1) {
		if _, err := strconv.Atoi(match[2]); err != nil {
			return fmt.Errorf("invalid duration %q: %w", s, err)
		}
		unit := strings.ToLower(strings.TrimSpace(match[3]))
		for _, variants := range timeUnits {
			last := len(variants) - 1
			for i, variant := range variants {
				if (last == i && strings.HasPrefix(unit, variant)) || strings.EqualFold(variant, unit) {
					matched = true
				}
			}
		}
	}
	if matched {
		return nil
	}
	return fmt.Errorf("%q is not a valid duration", s)
}

// validateDate is a port of kube-openapi strfmt.IsDate.
func validateDate(v any) error {
	s, ok := v.(string)
	if !ok {
		return nil
	}
	if _, err := time.Parse(rfc3339FullDate, s); err != nil {
		return fmt.Errorf("%q is not a valid date", s)
	}
	return nil
}

// validateDateTime is a port of kube-openapi strfmt.IsDateTime. The spelling
// is registered under both "datetime" and "date-time"; see
// registerKubernetesFormats.
func validateDateTime(v any) error {
	s, ok := v.(string)
	if !ok {
		return nil
	}
	if len(s) < 4 {
		return fmt.Errorf("%q is not a valid date-time", s)
	}
	parts := strings.Split(strings.ToLower(s), "t")
	if len(parts) < 2 {
		return fmt.Errorf("%q is not a valid date-time", s)
	}
	if _, err := time.Parse(rfc3339FullDate, parts[0]); err != nil {
		return fmt.Errorf("%q is not a valid date-time", s)
	}
	matches := rxDateTime.FindAllStringSubmatch(parts[1], -1)
	if len(matches) == 0 || len(matches[0]) == 0 {
		return fmt.Errorf("%q is not a valid date-time", s)
	}
	m := matches[0]
	if m[1] > "23" || m[2] > "59" || m[3] > "59" {
		return fmt.Errorf("%q is not a valid date-time", s)
	}
	return nil
}

// validateTime is permissive — kube-apiserver does not enforce format: time.
func validateTime(any) error {
	return nil
}
