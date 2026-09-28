// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/onsi/gomega"
	"sigs.k8s.io/yaml"

	apiv1 "github.com/fluxcd/flux-schema/api/v1beta1"
	"github.com/fluxcd/flux-schema/internal/junitxml"
)

func TestValidateCmd_Envsubst(t *testing.T) {
	for _, tt := range []struct {
		name      string
		stdin     bool
		defaulted bool
	}{
		{name: "file"},
		{name: "stdin", stdin: true},
		{name: "empty dotenv applies defaults", defaulted: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			dir := t.TempDir()
			vars := "# Post-build values\nexport REPLICAS=\"2\" # replicas\n"
			path := "../../internal/validator/testdata/envsubst/deployment.yaml"
			if tt.defaulted {
				raw, err := os.ReadFile(path)
				g.Expect(err).NotTo(HaveOccurred())
				path = writeManifest(t, dir, "deployment.yaml", strings.ReplaceAll(string(raw), "${REPLICAS}", "${REPLICAS:=2}"))
				vars = ""
			}
			dotenv := writeManifest(t, dir, ".env", vars)
			if tt.stdin {
				raw, err := os.ReadFile(path)
				g.Expect(err).NotTo(HaveOccurred())
				replaceStdin(t, string(raw))
				path = "-"
			}
			out, err := executeCommand([]string{
				"validate", path, "-s", "../../catalog/latest", "--envsubst", dotenv, "-v",
			})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(out).To(ContainSubstring("apps/v1/Deployment/apps/web is valid"))
			g.Expect(out).To(ContainSubstring("Valid: 1, Invalid: 0, Skipped: 0"))
			g.Expect(validateArgs.envsubst).To(BeEmpty())
		})
	}
}

func TestValidateCmd_EnvsubstConfig(t *testing.T) {
	for _, tt := range []struct {
		name      string
		absolute  bool
		configEnv bool
		override  string
		valid     bool
	}{
		{name: "relative to config", valid: true},
		{name: "absolute", absolute: true, valid: true},
		{name: "config from environment", configEnv: true, valid: true},
		{name: "CLI path relative to working directory", override: ".env", valid: true},
		{name: "CLI disables substitution", override: "disable"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			catalog, err := filepath.Abs("../../catalog/latest")
			g.Expect(err).NotTo(HaveOccurred())
			raw, err := os.ReadFile("../../internal/validator/testdata/envsubst/deployment.yaml")
			g.Expect(err).NotTo(HaveOccurred())
			configDir := t.TempDir()
			dotenv := writeManifest(t, configDir, ".env", "REPLICAS=2\n")
			if !tt.absolute {
				dotenv = ".env"
			}
			if tt.override != "" {
				dotenv = "missing.env"
			}
			cfg := writeManifest(t, configDir, ".fluxschema.yml", fmt.Sprintf(`apiVersion: schema.plugin.fluxcd.io/v1beta1
kind: Config
validate:
  envsubst: %q
`, dotenv))
			workingDir := t.TempDir()
			writeManifest(t, workingDir, "deployment.yaml", string(raw))
			writeManifest(t, workingDir, ".env", "export REPLICAS='3'\n")
			t.Chdir(workingDir)
			args := []string{"validate", "deployment.yaml", "-s", catalog, "-v"}
			if tt.configEnv {
				t.Setenv(envConfigFile, cfg)
			} else {
				args = append(args, "--config", cfg)
			}
			switch tt.override {
			case "disable":
				args = append(args, "--envsubst=")
			case ".env":
				args = append(args, "--envsubst", tt.override)
			}
			out, err := executeCommand(args)
			if tt.valid {
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(out).To(ContainSubstring("Valid: 1, Invalid: 0, Skipped: 0"))
			} else {
				g.Expect(err).To(MatchError(errSilent))
				g.Expect(out).To(ContainSubstring("schema violation"))
			}
		})
	}
}

func TestValidateCmd_EnvsubstStartupErrors(t *testing.T) {
	for _, tt := range []struct {
		name    string
		content string
		missing bool
		want    string
	}{
		{name: "missing file", missing: true, want: "envsubst: read dotenv file"},
		{name: "parse error", content: "VAR=\"unterminated", want: "envsubst: read dotenv file"},
		{name: "invalid variable name", content: "VAR.NAME=value\n", want: "var name is invalid"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			dir := t.TempDir()
			dotenv := filepath.Join(dir, ".env")
			if !tt.missing {
				writeManifest(t, dir, ".env", tt.content)
			}
			out, err := executeCommand([]string{
				"validate", "not-read.yaml", "--envsubst", dotenv, "-o", "json",
			})
			g.Expect(err).To(MatchError(ContainSubstring(tt.want)))
			g.Expect(out).To(BeEmpty())
		})
	}
}

func TestValidateCmd_EnvsubstReports(t *testing.T) {
	for _, output := range []string{"json", "yaml", "junit", "text"} {
		t.Run(output, func(t *testing.T) {
			g := NewWithT(t)
			dir := t.TempDir()
			dotenv := writeManifest(t, dir, ".env", "")
			raw := strings.Replace(validWidget, "name: hello", "name: '${'", 1)
			path := writeManifest(t, dir, "widget.yaml", raw)
			out, err := executeCommand([]string{
				"validate", path, "--envsubst", dotenv, "-o", output,
			})
			g.Expect(err).To(MatchError(errSilent))
			switch output {
			case "json", "yaml":
				var report apiv1.Report
				if output == "json" {
					validateReportSchema(t, out)
					report = decodeReport(t, out)
				} else {
					g.Expect(yaml.Unmarshal([]byte(out), &report)).To(Succeed())
				}
				g.Expect(report.Report.Summary).To(Equal(apiv1.ReportSummary{Total: 1, Invalid: 1}))
				g.Expect(report.Report.Results).To(HaveLen(1))
				result := report.Report.Results[0]
				g.Expect(result.Reason).To(Equal(apiv1.ReportReasonEnvsubstError))
				g.Expect(result.Status).To(Equal("invalid"))
				g.Expect(result.Resource).To(Equal(&apiv1.ReportResource{
					APIVersion: "example.com/v1", Kind: "Widget", Namespace: "default", Name: "ok-widget",
				}))
				g.Expect(result.Violations).To(HaveLen(1))
				g.Expect(result.Violations[0].Path).To(BeEmpty())
				g.Expect(result.Violations[0].Message).NotTo(BeEmpty())
			case "junit":
				var suites junitxml.TestSuites
				g.Expect(xml.Unmarshal([]byte(out), &suites)).To(Succeed())
				g.Expect(suites.Suites).To(HaveLen(1))
				g.Expect(suites.Suites[0].TestCases).To(HaveLen(1))
				tc := suites.Suites[0].TestCases[0]
				g.Expect(tc.Name).To(Equal("default/ok-widget"))
				g.Expect(tc.Failure).NotTo(BeNil())
				g.Expect(tc.Failure.Type).To(Equal("envsubst-error"))
				g.Expect(tc.Failure.Body).NotTo(BeEmpty())
			case "text":
				g.Expect(out).To(ContainSubstring("example.com/v1/Widget/default/ok-widget is invalid: envsubst error"))
			}
		})
	}
}

func TestValidateCmd_EnvsubstStrict(t *testing.T) {
	for _, tt := range []struct {
		name   string
		config string
		args   []string
		strict bool
	}{
		{name: "flag", args: []string{"--envsubst-strict"}, strict: true},
		{name: "config", config: "  envsubstStrict: true\n", strict: true},
		{name: "flag overrides config", config: "  envsubstStrict: true\n", args: []string{"--envsubst-strict=false"}},
		{name: "default"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			dir := t.TempDir()
			writeManifest(t, dir, ".env", "")
			cfg := writeManifest(t, dir, ".fluxschema.yml", `apiVersion: schema.plugin.fluxcd.io/v1beta1
kind: Config
validate:
  envsubst: .env
`+tt.config)
			args := append([]string{
				"validate", "../../internal/validator/testdata/envsubst/deployment.yaml",
				"-s", "../../catalog/latest", "--config", cfg,
			}, tt.args...)
			out, _ := executeCommand(args)
			if tt.strict {
				g.Expect(out).To(ContainSubstring("apps/v1/Deployment/apps/web is invalid: envsubst error"))
				g.Expect(out).To(ContainSubstring(`variable not set (strict mode): "REPLICAS"`))
			} else {
				g.Expect(out).NotTo(ContainSubstring("envsubst error"))
			}
		})
	}
}
