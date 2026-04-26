// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/hashicorp/go-retryablehttp"
	. "github.com/onsi/gomega"
)

const minimalOpenShiftSwagger = `{
  "swagger": "2.0",
  "definitions": {
    "com.github.openshift.api.route.v1.Route": {
      "type": "object",
      "properties": {
        "apiVersion": {"type": "string"},
        "kind":       {"type": "string"},
        "metadata":   {"type": "object"},
        "spec": {
          "type": "object",
          "properties": {"host": {"type": "string"}}
        }
      }
    },
    "com.github.openshift.api.cloudnetwork.v1.CloudPrivateIPConfig": {
      "type": "object",
      "properties": {
        "apiVersion": {"type": "string"},
        "kind":       {"type": "string"},
        "metadata":   {"type": "object"}
      }
    }
  }
}`

func writeOpenShiftSwaggerFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "openapi.json")
	if err := os.WriteFile(path, []byte(minimalOpenShiftSwagger), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

func TestExtractOpenShiftCmd_File(t *testing.T) {
	g := NewWithT(t)

	outDir := t.TempDir()
	input := writeOpenShiftSwaggerFixture(t)

	out, err := executeCommand([]string{"extract", "openshift", input, "--output-dir", outDir})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(ContainSubstring("reading " + input))
	g.Expect(out).To(ContainSubstring("OK   " + filepath.Join("route.openshift.io", "route_v1.json")))
	g.Expect(out).To(ContainSubstring("OK   " + filepath.Join("cloud.network.openshift.io", "cloudprivateipconfig_v1.json")))
	g.Expect(out).To(ContainSubstring("Summary: 2 schemas extracted"))

	data, err := os.ReadFile(filepath.Join(outDir, "route.openshift.io", "route_v1.json"))
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(string(data)).To(ContainSubstring(`"apiVersion"`))
	g.Expect(string(data)).To(ContainSubstring(`"kind"`))
	g.Expect(string(data)).ToNot(ContainSubstring(`"$schema"`))
}

func TestExtractOpenShiftCmd_OutputPathsAreOpenShiftOnly(t *testing.T) {
	// Walk every output file and assert each lives under <group>.openshift.io/.
	// The catalog-safety invariant: extract openshift must never write
	// outside the .openshift.io namespace, so a misconfigured generator
	// cannot overwrite a Kubernetes catalog entry.
	g := NewWithT(t)
	outDir := t.TempDir()
	input := writeOpenShiftSwaggerFixture(t)

	_, err := executeCommand([]string{"extract", "openshift", input, "--output-dir", outDir})
	g.Expect(err).ToNot(HaveOccurred())

	pathRe := regexp.MustCompile(`^[a-z][a-z0-9.]*\.openshift\.io/`)
	var written []string
	g.Expect(filepath.Walk(outDir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(outDir, p)
		written = append(written, filepath.ToSlash(rel))
		return nil
	})).To(Succeed())

	g.Expect(written).ToNot(BeEmpty())
	for _, p := range written {
		g.Expect(pathRe.MatchString(p)).To(BeTrue(), "path %q must start with <group>.openshift.io/", p)
	}
}

func TestExtractOpenShiftCmd_StdinDash(t *testing.T) {
	g := NewWithT(t)
	outDir := t.TempDir()
	replaceStdin(t, minimalOpenShiftSwagger)

	out, err := executeCommand([]string{"extract", "openshift", "-", "--output-dir", outDir})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(ContainSubstring("reading stdin"))
	_, err = os.Stat(filepath.Join(outDir, "route.openshift.io", "route_v1.json"))
	g.Expect(err).ToNot(HaveOccurred())
}

func TestExtractOpenShiftCmd_NoInput(t *testing.T) {
	g := NewWithT(t)
	forceStdinTTY(t)
	_, err := executeCommand([]string{"extract", "openshift"})
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("either a swagger file, piped stdin, or --ref is required"))
}

func TestExtractOpenShiftCmd_FileAndRef(t *testing.T) {
	g := NewWithT(t)
	input := writeOpenShiftSwaggerFixture(t)
	_, err := executeCommand([]string{"extract", "openshift", input, "--ref", "release-4.20"})
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("mutually exclusive"))
}

func TestExtractOpenShiftCmd_EmptyRef(t *testing.T) {
	g := NewWithT(t)
	forceStdinTTY(t)
	_, err := executeCommand([]string{"extract", "openshift", "--ref", ""})
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("--ref must not be empty"))
}

func TestExtractOpenShiftCmd_TwoFiles(t *testing.T) {
	g := NewWithT(t)
	input := writeOpenShiftSwaggerFixture(t)
	_, err := executeCommand([]string{"extract", "openshift", input, input})
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("at most 1 positional argument"))
}

func TestFetchOpenShiftSwagger_Success(t *testing.T) {
	g := NewWithT(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.Expect(r.URL.Path).To(Equal("/release-4.20/openapi.json"))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(minimalOpenShiftSwagger))
	}))
	defer srv.Close()

	client := retryablehttp.NewClient()
	client.Logger = nil

	tmpl := srv.URL + "/%s/openapi.json"
	url, body, err := fetchOpenShiftSwagger(context.Background(), client, tmpl, "release-4.20", 0)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(url).To(Equal(srv.URL + "/release-4.20/openapi.json"))
	g.Expect(body).To(ContainSubstring("Route"))
}

func TestFetchOpenShiftSwagger_NotFound(t *testing.T) {
	g := NewWithT(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "ref not found", http.StatusNotFound)
	}))
	defer srv.Close()

	client := retryablehttp.NewClient()
	client.Logger = nil

	url, _, err := fetchOpenShiftSwagger(context.Background(), client, srv.URL+"/%s/openapi.json", "release-9.99", 0)
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("fetch "))
	g.Expect(err.Error()).To(ContainSubstring("404"))
	g.Expect(url).To(ContainSubstring("release-9.99"))
}

func TestFetchOpenShiftSwagger_RejectsEmptyRef(t *testing.T) {
	g := NewWithT(t)

	client := retryablehttp.NewClient()
	client.Logger = nil

	_, _, err := fetchOpenShiftSwagger(context.Background(), client, "http://127.0.0.1/%s/openapi.json", "", 0)
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("--ref"))
}

func TestExtractOpenShiftCmd_RefFetch(t *testing.T) {
	g := NewWithT(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.Expect(r.URL.Path).To(Equal("/release-4.20/openapi.json"))
		_, _ = w.Write([]byte(minimalOpenShiftSwagger))
	}))
	defer srv.Close()

	client := newDefaultK8sHTTPClient()
	source, data, err := resolveOpenShiftInput(context.Background(), client, srv.URL+"/%s/openapi.json", "", "release-4.20", 0)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(source).To(Equal(srv.URL + "/release-4.20/openapi.json"))
	g.Expect(data).To(ContainSubstring("Route"))
}

func TestExtractOpenShiftCmd_AutoCreatesOutputDir(t *testing.T) {
	g := NewWithT(t)

	parent := t.TempDir()
	outDir := filepath.Join(parent, "nested", "out")
	input := writeOpenShiftSwaggerFixture(t)

	_, err := executeCommand([]string{"extract", "openshift", input, "--output-dir", outDir})
	g.Expect(err).ToNot(HaveOccurred())
	_, err = os.Stat(filepath.Join(outDir, "route.openshift.io", "route_v1.json"))
	g.Expect(err).ToNot(HaveOccurred())
}

func TestExtractOpenShiftCmd_StripDescription(t *testing.T) {
	g := NewWithT(t)
	outDir := t.TempDir()
	input := writeOpenShiftSwaggerFixture(t)

	_, err := executeCommand([]string{"extract", "openshift", input, "--output-dir", outDir, "--strip-description"})
	g.Expect(err).ToNot(HaveOccurred())

	data, err := os.ReadFile(filepath.Join(outDir, "route.openshift.io", "route_v1.json"))
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(string(data)).ToNot(ContainSubstring(`"description"`))
}

func TestExtractOpenShiftCmd_NonexistentFile(t *testing.T) {
	g := NewWithT(t)
	outDir := t.TempDir()
	_, err := executeCommand([]string{"extract", "openshift", filepath.Join(outDir, "missing.json"), "--output-dir", outDir})
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("read "))
}
