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

const refSwagger = `{
  "paths": {
    "/apis/example.com/v1/widgets": {
      "get": {
        "x-kubernetes-group-version-kind": {
          "group": "example.com",
          "version": "v1",
          "kind": "Widget"
        }
      }
    }
  },
  "definitions": {
    "example.v1.WidgetSpec": {
      "type": "object",
      "description": "WidgetSpec defines nested widget settings.",
      "properties": {
        "name": {"type": "string"}
      }
    },
    "example.v1.Widget": {
      "type": "object",
      "properties": {
        "spec": {
          "$ref": "#/definitions/example.v1.WidgetSpec",
          "description": "Spec configures the widget."
        }
      },
      "x-kubernetes-group-version-kind": [
        {"group": "example.com", "version": "v1", "kind": "Widget"}
      ]
    }
  }
}`

const minimalNamespacedSwagger = `{
  "paths": {
    "/apis/example.com/v1/namespaces/{namespace}/widgets": {
      "get": {
        "x-kubernetes-group-version-kind": {
          "group": "example.com",
          "version": "v1",
          "kind": "Widget"
        }
      }
    }
  },
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

const minimalListKindSwagger = `{
  "definitions": {
    "example.v1.WidgetList": {
      "type": "object",
      "properties": {
        "items": {
          "type": "array",
          "items": {"type": "object"}
        }
      },
      "x-kubernetes-group-version-kind": [
        {"group": "example.com", "version": "v1", "kind": "WidgetList"}
      ]
    }
  }
}`

func writeSwaggerFixture(t *testing.T) string {
	t.Helper()
	return writeSwaggerDataFixture(t, minimalSwagger)
}

func writeSwaggerDataFixture(t *testing.T, data string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "swagger.json")
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
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

func TestExtractK8sCmd_DefaultStripsExplainTypeMetadata(t *testing.T) {
	g := NewWithT(t)

	outDir := t.TempDir()
	input := writeSwaggerDataFixture(t, refSwagger)

	_, err := executeCommand([]string{"extract", "k8s", input, "--output-dir", outDir})
	g.Expect(err).ToNot(HaveOccurred())

	data, err := os.ReadFile(filepath.Join(outDir, "example.com", "widget_v1.json"))
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(string(data)).ToNot(ContainSubstring("x-flux-schema-type"))
	g.Expect(string(data)).ToNot(ContainSubstring("x-flux-schema-type-description"))
}

func TestExtractK8sCmd_WithExplainTypeMetadata(t *testing.T) {
	g := NewWithT(t)

	outDir := t.TempDir()
	input := writeSwaggerDataFixture(t, refSwagger)

	_, err := executeCommand([]string{
		"extract", "k8s", input,
		"--output-dir", outDir,
		"--with-explain-type-metadata",
	})
	g.Expect(err).ToNot(HaveOccurred())

	data, err := os.ReadFile(filepath.Join(outDir, "example.com", "widget_v1.json"))
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(string(data)).To(ContainSubstring(`"x-flux-schema-type": "WidgetSpec"`))
	g.Expect(string(data)).To(ContainSubstring(`"x-flux-schema-type-description": "WidgetSpec defines nested widget settings."`))
	g.Expect(string(data)).ToNot(ContainSubstring("x-flux-schema-group-version-kind"))
	g.Expect(string(data)).ToNot(ContainSubstring("x-flux-schema-resource"))

	_, err = os.Stat(filepath.Join(outDir, ".explain"))
	g.Expect(os.IsNotExist(err)).To(BeTrue())
	_, err = os.Stat(filepath.Join(outDir, "example.com", "widgets_v1.json"))
	g.Expect(os.IsNotExist(err)).To(BeTrue())
}

func TestExtractK8sCmd_WithExplainMetadataIncludesTypeAndLookupMetadata(t *testing.T) {
	g := NewWithT(t)

	outDir := t.TempDir()
	input := writeSwaggerDataFixture(t, refSwagger)

	_, err := executeCommand([]string{
		"extract", "k8s", input,
		"--output-dir", outDir,
		"--with-explain-metadata",
	})
	g.Expect(err).ToNot(HaveOccurred())

	data, err := os.ReadFile(filepath.Join(outDir, "example.com", "widget_v1.json"))
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(string(data)).To(ContainSubstring(`"x-flux-schema-type": "WidgetSpec"`))
	g.Expect(string(data)).To(ContainSubstring(`"x-flux-schema-type-description": "WidgetSpec defines nested widget settings."`))
	g.Expect(string(data)).To(ContainSubstring("x-flux-schema-group-version-kind"))
	g.Expect(string(data)).To(ContainSubstring("x-flux-schema-resource"))

	_, err = os.Stat(filepath.Join(outDir, ".explain", "refs", "widgets.json"))
	g.Expect(err).ToNot(HaveOccurred())
	_, err = os.Stat(filepath.Join(outDir, "example.com", "widgets_v1.json"))
	g.Expect(err).ToNot(HaveOccurred())
}

func TestExtractK8sCmd_WithFieldIndexNamespacedScope(t *testing.T) {
	g := NewWithT(t)

	outDir := t.TempDir()
	input := writeSwaggerDataFixture(t, minimalNamespacedSwagger)

	out, err := executeCommand([]string{"extract", "k8s", input, "--output-dir", outDir, "--with-field-index"})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(ContainSubstring("OK   " + filepath.Join("example.com", "widget_v1.fields.txt")))
	g.Expect(out).To(ContainSubstring("Summary: 1 schemas extracted"))

	data, err := os.ReadFile(filepath.Join(outDir, "example.com", "widget_v1.fields.txt"))
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(string(data)).To(HavePrefix("# schema source: Kubernetes\napiVersion <string> enum=example.com/v1\n"))
	g.Expect(string(data)).ToNot(ContainSubstring("flux-schema"))
	g.Expect(string(data)).To(ContainSubstring("metadata.namespace <string> (required)\n"))
}

func TestExtractK8sCmd_IndexSourceOverride(t *testing.T) {
	g := NewWithT(t)

	outDir := t.TempDir()
	input := writeSwaggerFixture(t)

	_, err := executeCommand([]string{
		"extract", "k8s", input,
		"--output-dir", outDir,
		"--with-field-index",
		"--index-source", "my-operator v1.2.3",
	})
	g.Expect(err).ToNot(HaveOccurred())

	data, err := os.ReadFile(filepath.Join(outDir, "example.com", "widget_v1.fields.txt"))
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(string(data)).To(HavePrefix("# schema source: my-operator v1.2.3\n"))
}

func TestExtractK8sCmd_WithFieldIndexUnknownScope(t *testing.T) {
	g := NewWithT(t)

	outDir := t.TempDir()
	input := writeSwaggerFixture(t)

	_, err := executeCommand([]string{"extract", "k8s", input, "--output-dir", outDir, "--with-field-index"})
	g.Expect(err).ToNot(HaveOccurred())

	data, err := os.ReadFile(filepath.Join(outDir, "example.com", "widget_v1.fields.txt"))
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(string(data)).To(ContainSubstring("metadata.namespace <string>\n"))
	g.Expect(string(data)).ToNot(ContainSubstring("metadata.namespace <string> (required)"))
	g.Expect(string(data)).ToNot(ContainSubstring("(cluster-scoped)"))
}

func TestExtractK8sCmd_WithFieldIndexSkipsListKind(t *testing.T) {
	g := NewWithT(t)

	outDir := t.TempDir()
	input := writeSwaggerDataFixture(t, minimalListKindSwagger)

	out, err := executeCommand([]string{"extract", "k8s", input, "--output-dir", outDir, "--with-field-index"})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(ContainSubstring("OK   " + filepath.Join("example.com", "widgetlist_v1.json")))
	g.Expect(out).ToNot(ContainSubstring("widgetlist_v1.fields.txt"))

	_, err = os.Stat(filepath.Join(outDir, "example.com", "widgetlist_v1.json"))
	g.Expect(err).ToNot(HaveOccurred())
	_, err = os.Stat(filepath.Join(outDir, "example.com", "widgetlist_v1.fields.txt"))
	g.Expect(os.IsNotExist(err)).To(BeTrue())
}

func TestExtractK8sCmd_StdinDash(t *testing.T) {
	g := NewWithT(t)
	outDir := t.TempDir()
	replaceStdin(t, minimalSwagger)

	out, err := executeCommand([]string{"extract", "k8s", "-", "--output-dir", outDir})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(ContainSubstring("reading stdin"))
	_, err = os.Stat(filepath.Join(outDir, "example.com", "widget_v1.json"))
	g.Expect(err).ToNot(HaveOccurred())
}

func TestExtractK8sCmd_StdinBare(t *testing.T) {
	g := NewWithT(t)
	outDir := t.TempDir()
	replaceStdin(t, minimalSwagger)

	_, err := executeCommand([]string{"extract", "k8s", "--output-dir", outDir})
	g.Expect(err).ToNot(HaveOccurred())
	_, err = os.Stat(filepath.Join(outDir, "example.com", "widget_v1.json"))
	g.Expect(err).ToNot(HaveOccurred())
}

func TestExtractK8sCmd_NoInput(t *testing.T) {
	g := NewWithT(t)
	forceStdinTTY(t)
	_, err := executeCommand([]string{"extract", "k8s"})
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("either a swagger file, piped stdin, or --version is required"))
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
	// Exercises the --version path via a monkey-patched URL template.
	g := NewWithT(t)

	var gotUserAgent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUserAgent = r.Header.Get("User-Agent")
		g.Expect(r.URL.Path).To(Equal("/v1.35.0/swagger.json"))
		_, _ = w.Write([]byte(minimalSwagger))
	}))
	defer srv.Close()

	client := newDefaultK8sHTTPClient()
	source, data, err := resolveK8sInput(context.Background(), client, srv.URL+"/%s/swagger.json", "", "1.35.0", 0)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(source).To(Equal(srv.URL + "/v1.35.0/swagger.json"))
	g.Expect(data).To(ContainSubstring("Widget"))
	g.Expect(gotUserAgent).To(Equal("flux-schema/0.0.0-dev.0"))
}

func TestK8sExtractWithVersionFallback(t *testing.T) {
	g := NewWithT(t)

	// The minimal swagger has no info.version: --version fills it, normalized.
	schemas, errs := k8sExtractWithVersionFallback("1.35.0")([]byte(minimalSwagger))
	g.Expect(errs).To(BeEmpty())
	g.Expect(schemas).ToNot(BeEmpty())
	g.Expect(schemas[0].Source).To(Equal("Kubernetes v1.35.0"))

	// A swagger info.version placeholder is discarded, then filled by --version.
	unversioned := `{"info": {"version": "unversioned"}, ` + minimalSwagger[1:]
	schemas, errs = k8sExtractWithVersionFallback("v1.34.1")([]byte(unversioned))
	g.Expect(errs).To(BeEmpty())
	g.Expect(schemas).ToNot(BeEmpty())
	g.Expect(schemas[0].Source).To(Equal("Kubernetes v1.34.1"))

	// A real swagger info.version wins over the flag.
	versioned := `{"info": {"version": "v1.33.0"}, ` + minimalSwagger[1:]
	schemas, errs = k8sExtractWithVersionFallback("v1.34.1")([]byte(versioned))
	g.Expect(errs).To(BeEmpty())
	g.Expect(schemas).ToNot(BeEmpty())
	g.Expect(schemas[0].Source).To(Equal("Kubernetes v1.33.0"))

	// No flag, no info.version: records only the source system.
	schemas, errs = k8sExtractWithVersionFallback("")([]byte(minimalSwagger))
	g.Expect(errs).To(BeEmpty())
	g.Expect(schemas).ToNot(BeEmpty())
	g.Expect(schemas[0].Source).To(Equal("Kubernetes"))
}
