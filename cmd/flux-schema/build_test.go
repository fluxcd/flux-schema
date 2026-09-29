// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/onsi/gomega"

	apiv1 "github.com/fluxcd/flux-schema/api/v1beta1"
)

func executeCommandStreams(args []string) (string, string, error) {
	defer resetCmdArgs()
	var stdout, stderr bytes.Buffer
	rootCmd.SetArgs(args)
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stderr)
	err := rootCmd.Execute()
	return stdout.String(), stderr.String(), err
}

func buildHeaders(output, prefix string) []string {
	var values []string
	for line := range strings.SplitSeq(output, "\n") {
		if value, ok := strings.CutPrefix(line, prefix); ok {
			values = append(values, value)
		}
	}
	return values
}

func TestBuildCmd_Inputs(t *testing.T) {
	const root = "testdata/validate/kustomize/"
	const repo = root + "flux-repo/"
	const base = repo + "apps/base/podinfo"
	const production = repo + "apps/production"
	const staging = repo + "apps/staging"
	const plain = repo + "clusters/staging/apps.yaml"
	origins := []string{base + "/release.yaml", base + "/repository.yaml"}
	for _, tt := range []struct {
		name    string
		paths   []string
		flags   []string
		stdin   string
		sources []string
		origins []string
		want    string
	}{
		{name: "plain file", paths: []string{plain}, sources: []string{plain}, want: "name: apps"},
		{name: "path as given", paths: []string{"./" + plain}, sources: []string{"./" + plain}},
		{name: "directory walk builds nested directories once", paths: []string{repo}, sources: []string{base, base, production, production, staging, staging, plain}, origins: append(append(append([]string{}, origins...), origins...), origins...), want: "replicaCount: 3"},
		{name: "explicit kustomization file", paths: []string{production + "/kustomization.yaml"}, sources: []string{production, production}, origins: origins, want: "replicaCount: 3"},
		{name: "explicit build directory", paths: []string{production + "/"}, sources: []string{production, production}, origins: origins},
		{name: "skip directory", paths: []string{repo}, flags: []string{"--skip-file", "apps"}, sources: []string{plain}},
		{name: "skip kustomization restores file walking", paths: []string{base}, flags: []string{"--skip-file", "kustomization.yaml"}, sources: origins},
		{name: "skip explicit kustomization renders file", paths: []string{base + "/kustomization.yaml"}, flags: []string{"--skip-file", "kustomization.yaml"}, sources: []string{base + "/kustomization.yaml"}, want: "resources:"},
		{name: "stdin", paths: []string{"-"}, stdin: validWidget, sources: []string{"stdin"}},
		{name: "implicit stdin", stdin: validWidget, sources: []string{"stdin"}},
		{name: "mixed stdin and files", paths: []string{plain, "-", base}, stdin: validWidget, sources: []string{plain, "stdin", base, base}, origins: origins},
		{name: "empty build", paths: []string{root + "empty"}},
		{name: "empty file source before build", paths: []string{"-", base}, stdin: "# empty\n", sources: []string{base, base}, origins: origins},
	} {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			if tt.stdin != "" {
				replaceStdin(t, tt.stdin)
			}
			args := append([]string{"build"}, tt.paths...)
			out, stderr, err := executeCommandStreams(append(args, tt.flags...))
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(stderr).To(BeEmpty())
			g.Expect(buildHeaders(out, "# Source: ")).To(Equal(tt.sources))
			g.Expect(buildHeaders(out, "# Origin: ")).To(Equal(tt.origins))
			g.Expect(strings.Count(out, "---\n")).To(Equal(len(tt.sources)))
			if tt.want != "" {
				g.Expect(out).To(ContainSubstring(tt.want))
			}
			g.Expect(out).NotTo(ContainSubstring("config.kubernetes.io/origin"))
		})
	}
}

func TestBuildCmd_Bytes(t *testing.T) {
	for _, tt := range []struct {
		name   string
		raw    string
		vars   string
		enable bool
		strict bool
		want   string
	}{
		{name: "plain bytes", raw: " \n# keep\nvalue: \"${VAR}\"  \n\n", want: "# keep\nvalue: \"${VAR}\"\n"},
		{name: "missing final newline", raw: "# keep\nvalue: 'x'", want: "# keep\nvalue: 'x'\n"},
		{name: "CRLF", raw: "# keep\r\nvalue: x\r\n", want: "# keep\r\nvalue: x\n"},
		{name: "substitution applied and typed", raw: "# dropped\nvalue: \"${VAR}\"\n", enable: true, vars: "VAR=2\n", want: "value: 2\n"},
		{name: "no dollar preserves comments", raw: "# keep\nvalue: 'x'\n", enable: true, want: "# keep\nvalue: 'x'\n"},
		{name: "empty dotenv enables defaults", raw: "value: \"${VAR:=true}\"\n", enable: true, strict: true, want: "value: true\n"},
		{name: "no dotenv ignores strict", raw: "value: '${VAR}'\n", strict: true, want: "value: '${VAR}'\n"},
		{name: "label disables and preserves bytes", raw: "# keep\nmetadata:\n  labels:\n    kustomize.toolkit.fluxcd.io/substitute: disabled\nvalue: \"${VAR}\"\n\n", enable: true, strict: true, want: "# keep\nmetadata:\n  labels:\n    kustomize.toolkit.fluxcd.io/substitute: disabled\nvalue: \"${VAR}\"\n"},
		{name: "annotation disables and preserves bytes", raw: "# keep\nmetadata:\n  annotations:\n    kustomize.toolkit.fluxcd.io/substitute: disabled\nvalue: \"${VAR}\"\n", enable: true, vars: "VAR=2\n", want: "# keep\nmetadata:\n  annotations:\n    kustomize.toolkit.fluxcd.io/substitute: disabled\nvalue: \"${VAR}\"\n"},
		{name: "malformed YAML without substitution passes through", raw: "value: [", want: "value: [\n"},
		{name: "malformed YAML with no dollar passes through", raw: "value: [", enable: true, want: "value: [\n"},
		{name: "leading marker", raw: "---\n# keep\nvalue: x\n", want: "# keep\nvalue: x\n"},
		{name: "leading BOM and marker", raw: "\ufeff---\n# keep\nvalue: x\n", want: "# keep\nvalue: x\n"},
		{name: "leading BOM", raw: "\ufeff# keep\nvalue: x\n", want: "# keep\nvalue: x\n"},
		{name: "marker comment", raw: "--- # keep\nvalue: x\n", want: "# keep\nvalue: x\n"},
		{name: "content-free", raw: "# keep nothing\n \n"},
		{name: "content-free with marker", raw: "---\n# keep nothing\n"},
		{name: "content-free with BOM", raw: "\ufeff---\n# keep nothing\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			dir := t.TempDir()
			path := writeManifest(t, dir, "manifest.yaml", tt.raw)
			args := []string{"build", path}
			if tt.enable {
				args = append(args, "--envsubst-file", writeManifest(t, dir, ".env", tt.vars))
			}
			if tt.strict {
				args = append(args, "--envsubst-strict")
			}
			out, stderr, err := executeCommandStreams(args)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(stderr).To(BeEmpty())
			want := tt.want
			if want != "" {
				want = "---\n# Source: " + path + "\n" + want
			}
			g.Expect(out).To(Equal(want))
			g.Expect(buildArgs).To(Equal(buildFlags{}))
		})
	}
}

func TestBuildCmd_DocumentOrderAndWhitespace(t *testing.T) {
	g := NewWithT(t)
	raw := "# empty\n---\n# first\nfirst: true  \n\n---\n  # empty\n---\nsecond: '2'\n---\nthird: true"
	replaceStdin(t, raw)
	out, stderr, err := executeCommandStreams([]string{"build", "-"})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(stderr).To(BeEmpty())
	g.Expect(out).To(Equal("---\n# Source: stdin\n# first\nfirst: true\n" +
		"---\n# Source: stdin\nsecond: '2'\n---\n# Source: stdin\nthird: true\n"))
}

func TestBuildCmd_SkipFiles(t *testing.T) {
	for _, tt := range []struct {
		name  string
		flags []string
		want  []string
	}{
		{name: "default hides dotfiles and directories", want: []string{"plain.yml"}},
		{name: "custom replaces default", flags: []string{"--skip-file", "plain.*"}, want: []string{".hidden.yaml", ".private/manifest.yaml"}},
		{name: "repeatable", flags: []string{"--skip-file", ".*", "--skip-file", "plain.*"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			dir := t.TempDir()
			g.Expect(os.Mkdir(filepath.Join(dir, ".private"), 0o755)).To(Succeed())
			writeManifest(t, dir, ".hidden.yaml", validWidget)
			writeManifest(t, dir, ".private/manifest.yaml", validWidget)
			writeManifest(t, dir, "plain.yml", validWidget)
			writeManifest(t, dir, "ignored.txt", validWidget)
			out, stderr, err := executeCommandStreams(append([]string{"build", dir}, tt.flags...))
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(stderr).To(BeEmpty())
			var want []string
			for _, name := range tt.want {
				want = append(want, filepath.Join(dir, name))
			}
			g.Expect(buildHeaders(out, "# Source: ")).To(Equal(want))
		})
	}
}

func TestBuildCmd_ContinuesAfterErrors(t *testing.T) {
	for _, tt := range []struct {
		name   string
		raw    string
		vars   string
		reason string
		msg    string
		build  bool
	}{
		{name: "strict failure", raw: "value: '${UNDEFINED}'\n", reason: "envsubst error", msg: `variable not set (strict mode): "UNDEFINED"`},
		{name: "malformed expression", raw: "value: '${'\n", reason: "envsubst error", msg: "variable substitution failed"},
		{name: "invalid input YAML", raw: "value: [${VAR}\n", reason: "yaml parse error", msg: "did not find expected"},
		{name: "duplicate key", raw: "value: '${VAR}'\nvalue: again\n", reason: "yaml parse error", msg: `key "value" already set in map`},
		{name: "invalid substituted YAML", raw: "value: '${VAR}'\n", vars: "VAR=[\n", reason: "yaml parse error", msg: "line"},
		{name: "kustomize build failure", build: true, reason: "kustomize build error", msg: "missing.yaml"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			dir := t.TempDir()
			before := writeManifest(t, dir, "before.yaml", "name: before\n")
			after := writeManifest(t, dir, "after.yaml", "name: after\n")
			bad := "testdata/validate/kustomize/broken"
			if !tt.build {
				bad = writeManifest(t, dir, "bad.yaml", tt.raw+"---\nname: next-document\n")
			}
			out, stderr, err := executeCommandStreams([]string{
				"build", before, bad, after, "--envsubst-file", writeManifest(t, dir, ".env", tt.vars), "--envsubst-strict",
			})
			g.Expect(err).To(MatchError(errSilent))
			prefix := "✗ " + bad + " - #1: "
			if tt.build {
				prefix = "✗ " + bad + ": "
			}
			g.Expect(stderr).To(HavePrefix(prefix + tt.reason + ": "))
			g.Expect(stderr).To(ContainSubstring(tt.msg))
			g.Expect(out).NotTo(ContainSubstring("value:"))
			want := "---\n# Source: " + before + "\nname: before\n"
			if !tt.build {
				want += "---\n# Source: " + bad + "\nname: next-document\n"
			}
			want += "---\n# Source: " + after + "\nname: after\n"
			g.Expect(out).To(Equal(want))
		})
	}
}

type buildFailingReader struct{}

func (buildFailingReader) Read([]byte) (int, error) {
	return 0, errors.New("read failed")
}

func TestBuildCmd_ReadFailure(t *testing.T) {
	g := NewWithT(t)
	orig := stdinReader
	stdinReader = io.MultiReader(strings.NewReader("name: first\n---\n"), buildFailingReader{})
	t.Cleanup(func() { stdinReader = orig })
	path := writeManifest(t, t.TempDir(), "after.yaml", "name: after\n")
	out, stderr, err := executeCommandStreams([]string{"build", "-", path})
	g.Expect(err).To(MatchError(errSilent))
	g.Expect(stderr).To(Equal("✗ stdin: source load error: scan stdin: read failed\n"))
	g.Expect(out).To(Equal("---\n# Source: stdin\nname: first\n---\n# Source: " + path + "\nname: after\n"))
}

func TestBuildCmd_FileReadFailure(t *testing.T) {
	g := NewWithT(t)
	dir := t.TempDir()
	bad := filepath.Join(dir, "a.yaml")
	g.Expect(os.Symlink(filepath.Join(dir, "missing"), bad)).To(Succeed())
	after := writeManifest(t, dir, "b.yaml", "name: after\n")
	out, stderr, err := executeCommandStreams([]string{"build", dir})
	g.Expect(err).To(MatchError(errSilent))
	g.Expect(stderr).To(HavePrefix("✗ " + bad + ": source load error: "))
	g.Expect(out).To(Equal("---\n# Source: " + after + "\nname: after\n"))
}

func TestBuildCmd_StartupErrors(t *testing.T) {
	for _, tt := range []struct {
		name  string
		flags []string
		env   string
		want  string
	}{
		{name: "missing dotenv", flags: []string{"--envsubst-file", "missing.env"}, want: "envsubst: read dotenv file missing.env"},
		{name: "dotenv parse error", env: "export VAR=1\n", want: "envsubst: read dotenv file"},
		{name: "invalid variable name", env: "VAR.NAME=value\n", want: `line 1: invalid variable name "VAR.NAME"`},
		{name: "invalid skip pattern", flags: []string{"--skip-file", "["}, want: "skip file pattern"},
		{name: "empty skip pattern", flags: []string{"--skip-file="}, want: "skip file pattern must not be empty"},
		{name: "no config flag", flags: []string{"--config", "config.yaml"}, want: "unknown flag: --config"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			dir := t.TempDir()
			path := writeManifest(t, dir, "valid.yaml", validWidget)
			args := []string{"build", path}
			if tt.env != "" {
				args = append(args, "--envsubst-file", writeManifest(t, dir, ".env", tt.env))
			}
			out, stderr, err := executeCommandStreams(append(args, tt.flags...))
			g.Expect(err).To(MatchError(ContainSubstring(tt.want)))
			g.Expect(out).To(BeEmpty())
			g.Expect(stderr).To(BeEmpty())
		})
	}
}

func TestBuildCmd_NoInput(t *testing.T) {
	for _, args := range [][]string{{"build"}, {"build", "-", "/dev/stdin"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			g := NewWithT(t)
			forceStdinTTY(t)
			out, stderr, err := executeCommandStreams(args)
			g.Expect(err).To(HaveOccurred())
			g.Expect(out).To(BeEmpty())
			g.Expect(stderr).To(BeEmpty())
		})
	}
}

func TestBuildCmd_IgnoresValidateConfig(t *testing.T) {
	g := NewWithT(t)
	t.Setenv(envConfigFile, "missing.config")
	path := writeManifest(t, t.TempDir(), "manifest.yaml", validWidget)
	out, stderr, err := executeCommandStreams([]string{"build", path})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(stderr).To(BeEmpty())
	g.Expect(out).To(Equal("---\n# Source: " + path + "\n" + validWidget))
}

func TestBuildCmd_ValidateRoundTrip(t *testing.T) {
	for _, tt := range []struct {
		name string
		path string
		vars string
		raw  string
	}{
		{name: "repository", path: "testdata/validate/kustomize/flux-repo"},
		{name: "substituted file", path: "../../internal/validator/testdata/envsubst/deployment.yaml", vars: "REPLICAS=2\n"},
		{name: "substituted build", path: "../../internal/validator/testdata/envsubst", vars: "REPLICAS=2\n"},
		{name: "BOM-prefixed document", raw: "\ufeff---\napiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: bom\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			path := tt.path
			if tt.raw != "" {
				path = writeManifest(t, t.TempDir(), "manifest.yaml", tt.raw)
			}
			var flags []string
			if tt.vars != "" {
				flags = []string{"--envsubst-file", writeManifest(t, t.TempDir(), ".env", tt.vars)}
			}
			out, stderr, err := executeCommandStreams(append([]string{"build", path}, flags...))
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(stderr).To(BeEmpty())
			direct, err := executeCommand(append([]string{"validate", path, "-s", "../../catalog/latest", "-o", "json"}, flags...))
			g.Expect(err).NotTo(HaveOccurred())
			replaceStdin(t, out)
			piped, err := executeCommand([]string{"validate", "-", "-s", "../../catalog/latest", "-o", "json"})
			g.Expect(err).NotTo(HaveOccurred())
			directReport := decodeReport(t, direct)
			pipedReport := decodeReport(t, piped)
			g.Expect(pipedReport.Report.Summary).To(Equal(directReport.Report.Summary))
			directResources := make([]*apiv1.ReportResource, 0, len(directReport.Report.Results))
			pipedResources := make([]*apiv1.ReportResource, 0, len(pipedReport.Report.Results))
			for _, result := range directReport.Report.Results {
				directResources = append(directResources, result.Resource)
			}
			for _, result := range pipedReport.Report.Results {
				pipedResources = append(pipedResources, result.Resource)
			}
			g.Expect(pipedResources).To(Equal(directResources))
		})
	}
}

func TestBuildCmd_MissingPath(t *testing.T) {
	g := NewWithT(t)
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.yaml")
	after := writeManifest(t, dir, "after.yaml", "name: after\n")
	out, stderr, err := executeCommandStreams([]string{"build", missing, after})
	g.Expect(err).To(MatchError(errSilent))
	g.Expect(stderr).To(HavePrefix("✗ " + missing + ": source load error: "))
	g.Expect(out).To(Equal("---\n# Source: " + after + "\nname: after\n"))
}

func TestBuildCmd_HeaderQuoting(t *testing.T) {
	g := NewWithT(t)
	g.Expect(headerValue("apps/base")).To(Equal("apps/base"))
	g.Expect(headerValue("a\nkind: Secret.yaml")).To(Equal(`"a\nkind: Secret.yaml"`))
}
