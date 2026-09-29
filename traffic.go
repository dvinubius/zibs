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
// an attribution hint, not authentication. "other" means unclassified traffic,
// not a verified human visitor.
func classifyTraffic(req *http.Request) trafficClass {
	if strings.HasPrefix(req.UserAgent(), syntheticUserAgentPrefix) {
		return trafficSynthetic
	}
	path := strings.ToLower(req.URL.Path)
	if strings.Contains(path, ".php") || strings.HasPrefix(path, "/wp-") ||
		path == "/xmlrpc" || path == "/xmlrpc/" {
		return trafficSuspectedScan
	}
	if req.Method == http.MethodPost {
		switch path {
		case "/api", "/graphql", "/_next", "/_rsc", "/rsc":
			return trafficSuspectedScan
		}
	}
	return trafficOther
}
