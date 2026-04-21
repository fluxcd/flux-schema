// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package validator

import (
	"testing"

	. "github.com/onsi/gomega"
)

func TestValidateDuration(t *testing.T) {
	// Accept cases cover both time.ParseDuration and the Scala-style regex
	// fallback. kube-openapi's regex matches any digit+letter substring and
	// accepts if at least one unit letter matches a known variant, so inputs
	// like "PT1H30M" and "-2w" are accepted — matching what apiextensions-
	// apiserver admits at CRD admission.
	accept := []string{
		"1h", "30m", "1h30m", "1h30m500ms", "1.5h", "-30m",
		"500ms", "1h 30m",
		"2w", "3d", "22 ns",
		"PT1H30M", "-2w", "1.5w",
	}
	reject := []string{
		"2y", "nope", "", "5",
	}

	for _, s := range accept {
		t.Run("accept/"+s, func(t *testing.T) {
			g := NewWithT(t)
			g.Expect(validateDuration(s)).To(Succeed())
		})
	}
	for _, s := range reject {
		t.Run("reject/"+s, func(t *testing.T) {
			g := NewWithT(t)
			g.Expect(validateDuration(s)).To(HaveOccurred())
		})
	}
}

func TestValidateDate(t *testing.T) {
	accept := []string{"2024-01-15", "1999-12-31", "2000-02-29"}
	reject := []string{"2024-13-01", "01-15-2024", "2024-1-5", "2024/01/15", "", "nope"}

	for _, s := range accept {
		t.Run("accept/"+s, func(t *testing.T) {
			g := NewWithT(t)
			g.Expect(validateDate(s)).To(Succeed())
		})
	}
	for _, s := range reject {
		t.Run("reject/"+s, func(t *testing.T) {
			g := NewWithT(t)
			g.Expect(validateDate(s)).To(HaveOccurred())
		})
	}
}

func TestValidateDateTime(t *testing.T) {
	accept := []string{
		"2024-01-15T10:30:00Z",
		"2024-01-15T10:30:00+02:00",
		"2024-01-15T10:30:00-05:30",
		"2024-01-15t10:30:00z",
		"2024-01-15T10:30:00.123Z",
	}
	reject := []string{
		"2024-01-15T10:30:00",
		"2024-01-15",
		"not a date",
		"",
		"2024-01-15T25:00:00Z",
		"2024-01-15T10:60:00Z",
	}

	for _, s := range accept {
		t.Run("accept/"+s, func(t *testing.T) {
			g := NewWithT(t)
			g.Expect(validateDateTime(s)).To(Succeed())
		})
	}
	for _, s := range reject {
		t.Run("reject/"+s, func(t *testing.T) {
			g := NewWithT(t)
			g.Expect(validateDateTime(s)).To(HaveOccurred())
		})
	}
}

func TestValidateTime_Permissive(t *testing.T) {
	g := NewWithT(t)
	// Any string passes; kube-apiserver does not register a "time" format.
	g.Expect(validateTime("not a time")).To(Succeed())
	g.Expect(validateTime("25:99:99")).To(Succeed())
	g.Expect(validateTime("")).To(Succeed())
}

func TestValidators_NonStringIsSilent(t *testing.T) {
	g := NewWithT(t)
	// jsonschema/v6 only calls format validators on strings, but defensively
	// non-string inputs must not error out.
	g.Expect(validateDuration(42)).To(Succeed())
	g.Expect(validateDate(nil)).To(Succeed())
	g.Expect(validateDateTime(true)).To(Succeed())
}
