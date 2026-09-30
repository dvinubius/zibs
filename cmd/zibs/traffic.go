package main

import (
	"net/http"
	"strings"
)

type trafficClass string

const (
	trafficSynthetic     trafficClass = "synthetic"
	trafficSuspectedScan trafficClass = "suspected_scan"
	trafficOther         trafficClass = "other"
)

const syntheticUserAgentPrefix = "zibs-traffic-lab/"

// classifyTraffic partitions requests for telemetry. The user-agent marker is
// an attribution hint, not authentication. A request that is neither an
// application route nor a possible short-link follow cannot be ordinary use,
// so probes, crawler files, and wrong methods are all suspected scans.
// "other" means unclassified traffic, not a verified human visitor.
func classifyTraffic(req *http.Request) trafficClass {
	if strings.HasPrefix(req.UserAgent(), syntheticUserAgentPrefix) {
		return trafficSynthetic
	}
	if routeLabel(req) != "/{code}" {
		return trafficOther
	}
	get := req.Method == http.MethodGet || req.Method == http.MethodHead
	if get && isShortCodePath(req.URL.Path) {
		return trafficOther
	}
	return trafficSuspectedScan
}

// isShortCodePath reports whether path could name a link. Every code ever
// issued has shortCodeLength characters from shortCodeAlphabet.
func isShortCodePath(path string) bool {
	code, ok := strings.CutPrefix(path, "/")
	if !ok || len(code) != shortCodeLength {
		return false
	}
	for _, c := range code {
		if !strings.ContainsRune(shortCodeAlphabet, c) {
			return false
		}
	}
	return true
}
