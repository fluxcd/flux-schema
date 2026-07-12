// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package useragent

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/go-retryablehttp"
	. "github.com/onsi/gomega"
)

func TestTransportSetsUserAgentWithNilNext(t *testing.T) {
	g := NewWithT(t)
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	client := &http.Client{Transport: Transport("flux-schema/test", nil)}
	req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
	g.Expect(err).ToNot(HaveOccurred())
	req.Header.Set("User-Agent", "original")

	resp, err := client.Do(req)
	g.Expect(err).ToNot(HaveOccurred())
	defer resp.Body.Close()

	g.Expect(resp.StatusCode).To(Equal(http.StatusNoContent))
	g.Expect(got).To(Equal("flux-schema/test"))
	g.Expect(req.Header.Get("User-Agent")).To(Equal("original"))
}

func TestWrapRetryableHTTPClient(t *testing.T) {
	g := NewWithT(t)
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	client := retryablehttp.NewClient()
	client.Logger = nil
	client.RetryMax = 0
	Wrap(client, "flux-schema/retryable")

	req, err := retryablehttp.NewRequest(http.MethodGet, srv.URL, nil)
	g.Expect(err).ToNot(HaveOccurred())
	resp, err := client.Do(req)
	g.Expect(err).ToNot(HaveOccurred())
	defer resp.Body.Close()

	g.Expect(resp.StatusCode).To(Equal(http.StatusNoContent))
	g.Expect(got).To(Equal("flux-schema/retryable"))
}
