package auth

import (
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strings"
)

const defaultHTTPPort = "80"

func (a *Auth) isAllowedOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Scheme != "http" || u.Host == "" || origin != "http://"+u.Host {
		return false
	}
	host, port := splitHostPort(u.Host)
	return a.isAllowedHost(host, port)
}

func (a *Auth) isAllowedHost(host, port string) bool {
	if host == "" || (!a.cfg.Dev && port != a.port) {
		return false
	}
	matchesHost := func(allowed string) bool { return strings.EqualFold(allowed, host) }
	return isLoopbackHost(host) || slices.ContainsFunc(a.currentHosts(), matchesHost)
}

func isLoopbackHost(host string) bool {
	return strings.EqualFold(host, "localhost") || host == "127.0.0.1" || host == "::1"
}

func (a *Auth) currentHosts() []string {
	now := a.cfg.Now()
	a.mu.Lock()
	hosts, fetchedAt := a.hosts, a.hostsFetchedAt
	a.mu.Unlock()
	if !fetchedAt.IsZero() && now.Sub(fetchedAt) < hostsCacheTTL {
		return hosts
	}

	hosts = a.cfg.Hosts()
	a.mu.Lock()
	a.hosts, a.hostsFetchedAt = hosts, now
	a.mu.Unlock()
	return hosts
}

func splitHostPort(hostport string) (host, port string) {
	u := url.URL{Host: hostport}
	port = u.Port()
	if port == "" {
		port = defaultHTTPPort
	}
	return u.Hostname(), port
}

func withForwardedRemote(r *http.Request) *http.Request {
	peer, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil || !peer.Addr().Unmap().IsLoopback() {
		return r
	}
	forwarded, ok := lastForwardedAddr(r.Header.Values("X-Forwarded-For"))
	if !ok {
		return r
	}
	forwardedRequest := r.WithContext(r.Context())
	forwardedRequest.RemoteAddr = netip.AddrPortFrom(forwarded, peer.Port()).String()
	return forwardedRequest
}

func lastForwardedAddr(headerValues []string) (netip.Addr, bool) {
	hops := strings.Split(strings.Join(headerValues, ","), ",")
	addr, err := netip.ParseAddr(strings.TrimSpace(hops[len(hops)-1]))
	if err != nil {
		return netip.Addr{}, false
	}
	return addr.Unmap(), true
}

func isLoopback(remoteAddr string) bool {
	addr, err := netip.ParseAddrPort(remoteAddr)
	return err == nil && addr.Addr().Unmap().IsLoopback()
}

func isSafeMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}
