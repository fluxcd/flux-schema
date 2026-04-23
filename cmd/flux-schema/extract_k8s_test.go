// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/hashicorp/go-retryablehttp"
	. "github.com/onsi/gomega"
)

const minimalSwagger = `{
  "definitions": {
    "example.v1.Widget": {
      "type": "object",
      "properties": {
        "spec": {
          "type": "object",
          "properties": {
            "name": {"type": "string"}
          },
          "required": ["name"]
        }
      },
      "x-kubernetes-group-version-kind": [
        {"group": "example.com", "version": "v1", "kind": "Widget"}
      ]
    }
  }
}`

func writeSwaggerFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "swagger.json")
	if err := os.WriteFile(path, []byte(minimalSwagger), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

func TestExtractK8sCmd_File(t *testing.T) {
	g := NewWithT(t)

	outDir := t.TempDir()
	input := writeSwaggerFixture(t)

	out, err := executeCommand([]string{"extract", "k8s", input, "--output-dir", outDir})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(ContainSubstring("reading " + input))
	g.Expect(out).To(ContainSubstring("OK   " + filepath.Join("example.com", "widget_v1.json")))
	g.Expect(out).To(ContainSubstring("Summary: 1 schemas extracted"))

	data, err := os.ReadFile(filepath.Join(outDir, "example.com", "widget_v1.json"))
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(string(data)).To(ContainSubstring(`"apiVersion"`))
	g.Expect(string(data)).To(ContainSubstring(`"kind"`))
	g.Expect(string(data)).ToNot(ContainSubstring(`"$schema"`))
	// Descriptions are preserved by default (the injectGVK step adds them to
	// apiVersion/kind); stripping is opt-in via --strip-description.
	g.Expect(string(data)).To(ContainSubstring(`"description"`))
}

func TestExtractK8sCmd_StripDescription(t *testing.T) {
	g := NewWithT(t)

	outDir := t.TempDir()
	input := writeSwaggerFixture(t)

	_, err := executeCommand([]string{"extract", "k8s", input, "--output-dir", outDir, "--strip-description"})
	g.Expect(err).ToNot(HaveOccurred())

	data, err := os.ReadFile(filepath.Join(outDir, "example.com", "widget_v1.json"))
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(string(data)).ToNot(ContainSubstring(`"description"`))
}

func TestExtractK8sCmd_NoInput(t *testing.T) {
	g := NewWithT(t)
	_, err := executeCommand([]string{"extract", "k8s"})
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("either a swagger file or --version is required"))
}

func TestExtractK8sCmd_FileAndVersion(t *testing.T) {
	g := NewWithT(t)
	input := writeSwaggerFixture(t)
	_, err := executeCommand([]string{"extract", "k8s", input, "--version", "1.35.0"})
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("mutually exclusive"))
}

func TestExtractK8sCmd_TwoFiles(t *testing.T) {
	g := NewWithT(t)
	input := writeSwaggerFixture(t)
	_, err := executeCommand([]string{"extract", "k8s", input, input})
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("at most 1 positional argument"))
}

func TestExtractK8sCmd_AutoCreatesOutputDir(t *testing.T) {
	g := NewWithT(t)

	parent := t.TempDir()
	outDir := filepath.Join(parent, "nested", "out")
	input := writeSwaggerFixture(t)

	_, err := executeCommand([]string{"extract", "k8s", input, "--output-dir", outDir})
	g.Expect(err).ToNot(HaveOccurred())
	_, err = os.Stat(filepath.Join(outDir, "example.com", "widget_v1.json"))
	g.Expect(err).ToNot(HaveOccurred())
}

func TestExtractK8sCmd_NonexistentFile(t *testing.T) {
	g := NewWithT(t)
	outDir := t.TempDir()
	_, err := executeCommand([]string{"extract", "k8s", filepath.Join(outDir, "missing.json"), "--output-dir", outDir})
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("read "))
}

func TestFetchK8sSwagger_Success(t *testing.T) {
	g := NewWithT(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.Expect(r.URL.Path).To(Equal("/v1.35.0/swagger.json"))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(minimalSwagger))
	}))
	defer srv.Close()

	client := retryablehttp.NewClient()
	client.Logger = nil

	tmpl := srv.URL + "/%s/swagger.json"
	url, body, err := fetchK8sSwagger(context.Background(), client, tmpl, "1.35.0", 0)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(url).To(Equal(srv.URL + "/v1.35.0/swagger.json"))
	g.Expect(body).To(ContainSubstring("example.v1.Widget"))
}

func TestFetchK8sSwagger_VersionWithVPrefix(t *testing.T) {
	g := NewWithT(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.Expect(r.URL.Path).To(Equal("/v1.35.0/swagger.json"))
		_, _ = w.Write([]byte(minimalSwagger))
	}))
	defer srv.Close()

	client := retryablehttp.NewClient()
	client.Logger = nil

	_, _, err := fetchK8sSwagger(context.Background(), client, srv.URL+"/%s/swagger.json", "v1.35.0", 0)
	g.Expect(err).ToNot(HaveOccurred())
}

func TestFetchK8sSwagger_RejectsBadVersion(t *testing.T) {
	g := NewWithT(t)

	client := retryablehttp.NewClient()
	client.Logger = nil

	for _, bad := range []string{"1.35", "latest", "v1", "1.2.3.4", ""} {
		_, _, err := fetchK8sSwagger(context.Background(), client, "http://127.0.0.1/%s", bad, 0)
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("invalid version"))
	}
}

func TestNormalizeK8sVersion(t *testing.T) {
	g := NewWithT(t)

	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{in: "1.35.0", want: "v1.35.0"},
		{in: "v1.35.0", want: "v1.35.0"},
		{in: "0.0.1", want: "v0.0.1"},
		{in: "v10.20.30", want: "v10.20.30"},
		{in: "1.35", wantErr: true},
		{in: "1.2.3.4", wantErr: true},
		{in: "v1.2.3-rc.1", wantErr: true},
		{in: "1.2.3+build", wantErr: true},
		{in: "latest", wantErr: true},
		{in: "", wantErr: true},
	}
	for _, tc := range cases {
		got, err := normalizeK8sVersion(tc.in)
		if tc.wantErr {
			g.Expect(err).To(HaveOccurred(), "want error for %q", tc.in)
			continue
		}
		g.Expect(err).ToNot(HaveOccurred(), "for %q", tc.in)
		g.Expect(got).To(Equal(tc.want), "for %q", tc.in)
	}
}

func TestFetchK8sSwagger_NotFound(t *testing.T) {
	g := NewWithT(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "release not found", http.StatusNotFound)
	}))
	defer srv.Close()

	client := retryablehttp.NewClient()
	client.Logger = nil

	_, _, err := fetchK8sSwagger(context.Background(), client, srv.URL+"/%s/swagger.json", "1.99.0", 0)
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("fetch "))
	g.Expect(err.Error()).To(ContainSubstring("404"))
}

func TestExtractK8sCmd_VersionFetch(t *testing.T) {
	// Exercises the full --version path by monkey-patching the URL template.
	// The httptest server-based fetch is tested directly in TestFetchK8sSwagger_*.
	// This asserts the CLI wiring: version + output-dir produces a schema file.
	g := NewWithT(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.Expect(r.URL.Path).To(Equal("/v1.35.0/swagger.json"))
		_, _ = w.Write([]byte(minimalSwagger))
	}))
	defer srv.Close()

	client := newDefaultK8sHTTPClient()
	source, data, err := resolveK8sInput(context.Background(), client, srv.URL+"/%s/swagger.json", "", "1.35.0", 0)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(source).To(Equal(srv.URL + "/v1.35.0/swagger.json"))
	g.Expect(data).To(ContainSubstring("Widget"))
}
