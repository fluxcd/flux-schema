// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/hashicorp/go-retryablehttp"
	"github.com/spf13/cobra"

	"github.com/fluxcd/flux-schema/internal/extractor"
	"github.com/fluxcd/flux-schema/internal/tmpl"
)

const (
	defaultExtractK8sFormat = "{{ .Group }}/{{ .Kind }}_{{ .Version }}.json"
	defaultK8sSwaggerURL    = "https://raw.githubusercontent.com/kubernetes/kubernetes/%s/api/openapi-spec/swagger.json"
)

var extractK8sCmd = &cobra.Command{
	Use:   "k8s [swagger-file]",
	Short: "Extract JSON Schemas from a Kubernetes OpenAPI v2 swagger document",
	Example: `  # Fetch upstream swagger for a release version
  flux-schema extract k8s --version 1.35.0 -d ./schemas

  # Read a local swagger file
  kubectl get --raw /openapi/v2 > swagger.json
  flux-schema extract k8s swagger.json -d ./schemas

  # Pipe from stdin
  kubectl get --raw /openapi/v2 | flux-schema extract k8s /dev/stdin -d ./schemas`,
	Args: requireFileOrVersion,
	RunE: extractK8sCmdRun,
}

type extractK8sFlags struct {
	outputDir    string
	outputFormat string
	k8sVersion   string
}

var extractK8sArgs = extractK8sFlags{
	outputDir:    ".",
	outputFormat: defaultExtractK8sFormat,
}

func init() {
	extractK8sCmd.Flags().StringVarP(&extractK8sArgs.outputDir, "output-dir", "d", extractK8sArgs.outputDir,
		"directory where JSON Schema files are written (created if missing)")
	extractK8sCmd.Flags().StringVarP(&extractK8sArgs.outputFormat, "output-format", "f", defaultExtractK8sFormat,
		"Go template for the output file path, relative to --output-dir; "+
			"variables: .Group, .GroupPrefix, .Kind, .Version")
	extractK8sCmd.Flags().StringVar(&extractK8sArgs.k8sVersion, "version", "",
		"Kubernetes release tag (e.g. 1.35.0) to fetch the upstream swagger from kubernetes/kubernetes")
	_ = extractK8sCmd.MarkFlagDirname("output-dir")
	extractCmd.AddCommand(extractK8sCmd)
}

var k8sVersionRe = regexp.MustCompile(`^v?\d+\.\d+\.\d+$`)

// requireFileOrVersion enforces mutual exclusion between a single positional
// swagger file and --version. Cobra's MarkFlagsMutuallyExclusive only covers
// flag pairs.
func requireFileOrVersion(cmd *cobra.Command, args []string) error {
	if len(args) > 1 {
		return fmt.Errorf("accepts at most 1 positional argument, received %d", len(args))
	}
	hasVersion := extractK8sArgs.k8sVersion != ""
	switch {
	case hasVersion && len(args) == 1:
		return fmt.Errorf("--version and a swagger file are mutually exclusive")
	case !hasVersion && len(args) == 0:
		return fmt.Errorf("either a swagger file or --version is required")
	}
	return nil
}

func extractK8sCmdRun(cmd *cobra.Command, args []string) error {
	var arg string
	if len(args) == 1 {
		arg = args[0]
	}
	client := newDefaultK8sHTTPClient()
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	source, data, err := resolveK8sInput(ctx, client, defaultK8sSwaggerURL, arg, extractK8sArgs.k8sVersion, rootArgs.timeout)
	if err != nil {
		return err
	}

	destDir := extractK8sArgs.outputDir
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	cmd.Printf("reading %s\n", source)

	schemas, errs := extractor.ExtractOpenAPI(data)

	var failures []error
	for _, e := range errs {
		failures = append(failures, fmt.Errorf("%s: %w", source, e))
	}

	written := 0
	for _, schema := range schemas {
		relPath, err := writeK8sSchema(schema, destDir)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		cmd.Printf("OK   %s\n", relPath)
		written++
	}

	cmd.Printf("Summary: %d schemas extracted\n", written)

	if len(failures) > 0 {
		for _, e := range failures {
			cmd.PrintErrf("FAIL %v\n", e)
		}
		return fmt.Errorf("%d error(s) during extraction", len(failures))
	}
	return nil
}

func writeK8sSchema(schema extractor.Schema, destDir string) (string, error) {
	rendered, err := tmpl.Render(extractK8sArgs.outputFormat, tmpl.SchemaVars{
		Group:   schema.Group,
		Kind:    schema.Kind,
		Version: schema.Version,
	})
	if err != nil {
		return "", fmt.Errorf("%s/%s %s: %w", schema.Group, schema.Kind, schema.Version, err)
	}

	relPath := filepath.FromSlash(rendered)
	outPath := filepath.Join(destDir, relPath)
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return "", fmt.Errorf("create %s: %w", filepath.Dir(outPath), err)
	}

	payload, err := marshalSchema(schema.Schema)
	if err != nil {
		return "", fmt.Errorf("%s %s: %w", schema.Kind, schema.Version, err)
	}
	if err := os.WriteFile(outPath, payload, 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", outPath, err)
	}
	return relPath, nil
}

func newDefaultK8sHTTPClient() *retryablehttp.Client {
	c := retryablehttp.NewClient()
	c.Logger = nil
	return c
}

// resolveK8sInput returns (source, data). source is the file path or URL used
// only as the log header. Exactly one of arg / version is populated by the
// time this runs (requireFileOrVersion has already enforced that).
func resolveK8sInput(ctx context.Context, client *retryablehttp.Client,
	urlTemplate, arg, version string, timeout time.Duration,
) (string, []byte, error) {
	if arg != "" {
		data, err := os.ReadFile(arg)
		if err != nil {
			return "", nil, fmt.Errorf("read %s: %w", arg, err)
		}
		return arg, data, nil
	}
	return fetchK8sSwagger(ctx, client, urlTemplate, version, timeout)
}

// fetchK8sSwagger downloads the Kubernetes OpenAPI v2 swagger document for the
// given release version. Returns (url, body, err) — url is the rendered
// location used as the log header. Non-2xx responses are hard errors, unlike
// the validator loader's 404-as-soft-miss.
func fetchK8sSwagger(ctx context.Context, client *retryablehttp.Client,
	urlTemplate, version string, timeout time.Duration,
) (string, []byte, error) {
	normalized, err := normalizeK8sVersion(version)
	if err != nil {
		return "", nil, err
	}
	url := fmt.Sprintf(urlTemplate, normalized)

	req, err := retryablehttp.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return url, nil, err
	}
	if timeout > 0 {
		reqCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		req = req.WithContext(reqCtx)
	}
	resp, err := client.Do(req)
	if err != nil {
		return url, nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return url, nil, fmt.Errorf("fetch %s: %s", url, resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return url, nil, fmt.Errorf("read %s: %w", url, err)
	}
	return url, body, nil
}

func normalizeK8sVersion(version string) (string, error) {
	if !k8sVersionRe.MatchString(version) {
		return "", fmt.Errorf("invalid version %q: expected X.Y.Z or vX.Y.Z", version)
	}
	if !strings.HasPrefix(version, "v") {
		return "v" + version, nil
	}
	return version, nil
}
