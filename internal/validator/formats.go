// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package validator

import (
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"k8s.io/kube-openapi/pkg/validation/strfmt"
)

// registerKubernetesFormats installs format validators whose accept/reject
// semantics match kube-apiserver.
//
// jsonschema/v6 disables format assertions by default for draft/2020-12;
// AssertFormat promotes format violations from annotations to errors.
func registerKubernetesFormats(c *jsonschema.Compiler) {
	c.AssertFormat()

	c.RegisterFormat(&jsonschema.Format{Name: "duration", Validate: validateDuration})
	c.RegisterFormat(&jsonschema.Format{Name: "date", Validate: validateDate})

	// kube-openapi registers IsDateTime under "datetime" (no hyphen); we
	// register the same handler under both spellings so schemas using either
	// form behave identically.
	c.RegisterFormat(&jsonschema.Format{Name: "datetime", Validate: validateDateTime})
	c.RegisterFormat(&jsonschema.Format{Name: "date-time", Validate: validateDateTime})

	// kube-apiserver does not register a "time" format; install a permissive
	// no-op so schemas declaring format: time accept any string.
	c.RegisterFormat(&jsonschema.Format{Name: "time", Validate: validateTime})
}

func validateDuration(v any) error {
	s, ok := v.(string)
	if !ok {
		return nil
	}
	if !strfmt.IsDuration(s) {
		return fmt.Errorf("%q is not a valid duration", s)
	}
	return nil
}

func validateDate(v any) error {
	s, ok := v.(string)
	if !ok {
		return nil
	}
	if !strfmt.IsDate(s) {
		return fmt.Errorf("%q is not a valid date", s)
	}
	return nil
}

func validateDateTime(v any) error {
	s, ok := v.(string)
	if !ok {
		return nil
	}
	if !strfmt.IsDateTime(s) {
		return fmt.Errorf("%q is not a valid date-time", s)
	}
	return nil
}

// validateTime is permissive — kube-apiserver does not enforce format: time.
func validateTime(any) error {
	return nil
}
