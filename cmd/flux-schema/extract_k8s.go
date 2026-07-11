// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/hashicorp/go-retryablehttp"
	"github.com/spf13/cobra"

	"github.com/fluxcd/flux-schema/internal/extractor"
	"github.com/fluxcd/flux-schema/internal/flags"
	"github.com/fluxcd/flux-schema/internal/useragent"
)

const defaultK8sSwaggerURL = "https://raw.githubusercontent.com/kubernetes/kubernetes/%s/api/openapi-spec/swagger.json"

var extractK8sCmd = &cobra.Command{
	Use:     "k8s [swagger-file]",
	Aliases: []string{"kubernetes"},
	Short:   "Extract JSON Schemas from a Kubernetes OpenAPI v2 swagger document",
	Example: `  # Fetch upstream swagger for a release version
  flux-schema extract k8s --version 1.35.0 -d ./schemas

  # Read a local swagger file
  kubectl get --raw /openapi/v2 > swagger.json
  flux-schema extract k8s swagger.json -d ./schemas

  # Pipe from stdin
  kubectl get --raw /openapi/v2 | flux-schema extract k8s -d ./schemas`,
	Args: requireFileOrVersion,
	RunE: extractK8sCmdRun,
}

type extractK8sFlags struct {
	flags.ExtractOutput
	k8sVersion string
}

var extractK8sArgs = extractK8sFlags{
	ExtractOutput: flags.NewExtractOutput(),
}

func init() {
	extractK8sArgs.Register(extractK8sCmd)
	extractK8sCmd.Flags().StringVar(&extractK8sArgs.k8sVersion, "version", "",
		"Kubernetes release tag (e.g. 1.35.0) to fetch the upstream swagger from kubernetes/kubernetes")
	extractCmd.AddCommand(extractK8sCmd)
}

var k8sVersionRe = regexp.MustCompile(`^v?\d+\.\d+\.\d+$`)

// requireFileOrVersion enforces mutual exclusion between a single positional
// swagger file (or piped stdin) and --version. Cobra's
// MarkFlagsMutuallyExclusive only covers flag pairs.
func requireFileOrVersion(cmd *cobra.Command, args []string) error {
	if len(args) > 1 {
		return fmt.Errorf("accepts at most 1 positional argument, received %d", len(args))
	}
	hasVersion := extractK8sArgs.k8sVersion != ""
	switch {
	case hasVersion && len(args) == 1:
		return fmt.Errorf("--version and a swagger file are mutually exclusive")
	case !hasVersion && len(args) == 0 && !stdinIsPiped():
		return fmt.Errorf("either a swagger file, piped stdin, or --version is required")
	}
	return nil
}

func extractK8sCmdRun(cmd *cobra.Command, args []string) error {
	// --version mode skips positional input and falls through to fetchK8sSwagger.
	var arg string
	if extractK8sArgs.k8sVersion == "" {
		inputs, err := resolveStdinArgs(args)
		if err != nil {
			return err
		}
		arg = inputs[0]
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

	return runSwaggerExtract(cmd, source, data, extractK8sArgs.ExtractOutput,
		k8sExtractWithVersionFallback(extractK8sArgs.k8sVersion))
}

// k8sExtractWithVersionFallback wraps ExtractKubernetes so that when the swagger
// document carries no usable info.version, the release requested via
// --version is recorded as the schemas' source instead.
func k8sExtractWithVersionFallback(version string) func([]byte) ([]extractor.Schema, []error) {
	normalized := ""
	if version != "" {
		if v, err := normalizeK8sVersion(version); err == nil {
			normalized = v
		}
	}
	return func(data []byte) ([]extractor.Schema, []error) {
		schemas, errs := extractor.ExtractKubernetes(data)
		for i := range schemas {
			if schemas[i].Source == "Kubernetes" && normalized != "" {
				schemas[i].Source = "Kubernetes " + normalized
			}
		}
		return schemas, errs
	}
}

func newDefaultK8sHTTPClient() *retryablehttp.Client {
	c := retryablehttp.NewClient()
	c.Logger = nil
	useragent.Wrap(c, userAgent())
	return c
}

// resolveK8sInput returns (source, data). source is the file path or URL used
// only as the log header. Exactly one of arg / version is populated by the
// time this runs (requireFileOrVersion has already enforced that).
func resolveK8sInput(ctx context.Context, client *retryablehttp.Client,
	urlTemplate, arg, version string, timeout time.Duration,
) (string, []byte, error) {
	if arg != "" {
		data, err := readSource(arg)
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
	body, err := fetchURL(ctx, client, url, timeout)
	return url, body, err
}

// fetchURL performs a GET against url and returns the response body. Non-2xx
// responses are hard errors; callers that want to interpret a 404 as a soft
// miss should use a different code path.
func fetchURL(ctx context.Context, client *retryablehttp.Client,
	url string, timeout time.Duration,
) ([]byte, error) {
	req, err := retryablehttp.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if timeout > 0 {
		reqCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		req = req.WithContext(reqCtx)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("fetch %s: %s", url, resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", url, err)
	}
	return body, nil
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
