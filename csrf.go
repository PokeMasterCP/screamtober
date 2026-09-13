package main

import (
	"net"
	"net/http"
	"net/url"
	"strings"
)

func protectCrossOrigin(next http.Handler) http.Handler {
	protection := http.NewCrossOriginProtection()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := protection.Check(r); err != nil && !originMatchesHost(r) {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Browsers omit Sec-Fetch-Site for HTTP private-IP origins. Go then compares
// Origin's host to Host as strings, which fails for equivalent IP forms and
// default ports. Origin: null stays rejected.
func originMatchesHost(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" || origin == "null" {
		return false
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil {
		return false
	}
	return sameHostPort(parsed.Host, schemeDefaultPort(parsed.Scheme), r.Host)
}

func schemeDefaultPort(scheme string) string {
	if strings.EqualFold(scheme, "https") {
		return "443"
	}
	return "80"
}

func sameHostPort(originHost, originDefaultPort, requestHost string) bool {
	originName, originPort, ok := splitHostPortDefault(originHost, originDefaultPort)
	if !ok {
		return false
	}
	requestName, requestPort, ok := splitHostPortDefault(requestHost, "80")
	if !ok || originPort != requestPort {
		return false
	}
	if originIP := net.ParseIP(originName); originIP != nil {
		requestIP := net.ParseIP(requestName)
		return requestIP != nil && originIP.Equal(requestIP)
	}
	return strings.EqualFold(originName, requestName)
}

func splitHostPortDefault(host, defaultPort string) (string, string, bool) {
	if host == "" {
		return "", "", false
	}
	name, port, err := net.SplitHostPort(host)
	if err == nil {
		return name, port, name != "" && port != ""
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.String(), defaultPort, true
	}
	if strings.Contains(host, ":") {
		return "", "", false
	}
	return host, defaultPort, true
}
