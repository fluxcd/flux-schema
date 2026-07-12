// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

// Package useragent provides HTTP transport helpers for setting User-Agent.
package useragent

import (
	"net/http"

	"github.com/hashicorp/go-retryablehttp"
)

type transport struct {
	agent string
	next  http.RoundTripper
}

// Transport returns a RoundTripper that sets User-Agent on a cloned request
// before delegating to next. When next is nil, http.DefaultTransport is used.
func Transport(agent string, next http.RoundTripper) http.RoundTripper {
	if next == nil {
		next = http.DefaultTransport
	}
	return transport{agent: agent, next: next}
}

func (t transport) RoundTrip(req *http.Request) (*http.Response, error) {
	cloned := req.Clone(req.Context())
	if cloned.Header == nil {
		cloned.Header = make(http.Header)
	}
	cloned.Header.Set("User-Agent", t.agent)
	return t.next.RoundTrip(cloned)
}

// Wrap configures c to set User-Agent on outbound requests.
func Wrap(c *retryablehttp.Client, agent string) {
	if c.HTTPClient == nil {
		c.HTTPClient = &http.Client{}
	}
	c.HTTPClient.Transport = Transport(agent, c.HTTPClient.Transport)
}
