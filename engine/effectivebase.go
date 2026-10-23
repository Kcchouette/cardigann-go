package engine

import (
	"net/url"
	"strings"
)

// EffectiveBaseURL returns the base used to resolve request paths and
// extracted hrefs: the configured base, or the origin observed after a
// cross-origin redirect (domain rotation). Safe for concurrent use.
func (p *Provider) EffectiveBaseURL() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.effectiveBase
}

// observeFinalURL records a cross-origin redirect so subsequent requests and
// href absolutization target the new origin. Call it after every successful
// client.Do with resp.Request.URL (the final post-redirect URL).
//
//   - nil / non-http(s) → no-op
//   - final origin == configured origin and effective already configured → no-op
//   - final origin == configured origin but effective differs (redirect back)
//     → effectiveBase resets to the configured base (never go stale)
//   - otherwise → configured base with Scheme/Host taken from final
//     (configured path prefix preserved: a rotation is an origin swap)
func (p *Provider) observeFinalURL(final *url.URL) {
	if final == nil || (final.Scheme != "http" && final.Scheme != "https") {
		return
	}
	configured, err := url.Parse(p.baseURL)
	if err != nil {
		return
	}
	if sameOrigin(configured, final) {
		p.mu.Lock()
		if p.effectiveBase != p.baseURL {
			p.effectiveBase = p.baseURL
		}
		p.mu.Unlock()
		return
	}
	rotated := *configured
	rotated.Scheme = final.Scheme
	rotated.Host = final.Host
	next := rotated.String()
	p.mu.Lock()
	p.effectiveBase = next
	p.mu.Unlock()
}

// absolutizeHref resolves an href extracted from a page against the effective
// base and rewrites absolute URLs still pointing at the configured origin to
// the effective origin. Third-party origins (protectors, CDNs, file hosts)
// are never rewritten.
func (p *Provider) absolutizeHref(href string) string {
	if href == "" {
		return href
	}
	ref, err := url.Parse(href)
	if err != nil {
		return href
	}
	base := p.EffectiveBaseURL()
	bu, err := url.Parse(base)
	if err != nil {
		return href
	}
	if !ref.IsAbs() {
		// Relative, query-only, or protocol-relative (//host/path).
		return bu.ResolveReference(ref).String()
	}
	if ref.Scheme != "http" && ref.Scheme != "https" {
		return href
	}
	configured, err := url.Parse(p.baseURL)
	if err != nil {
		return href
	}
	if sameOrigin(ref, configured) {
		ref.Scheme = bu.Scheme
		ref.Host = bu.Host
		return ref.String()
	}
	return href
}

// sameOrigin reports whether a and b share scheme and canonical host
// (lowercased, scheme-default ports stripped). Path and query are ignored.
func sameOrigin(a, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) && canonicalHost(a) == canonicalHost(b)
}

// canonicalHost returns the lowercased host with the scheme-default port
// (:80 for http, :443 for https) stripped, so http://x and http://x:80
// compare equal.
func canonicalHost(u *url.URL) string {
	host := strings.ToLower(u.Host)
	switch {
	case u.Scheme == "http" && strings.HasSuffix(host, ":80"):
		return strings.TrimSuffix(host, ":80")
	case u.Scheme == "https" && strings.HasSuffix(host, ":443"):
		return strings.TrimSuffix(host, ":443")
	default:
		return host
	}
}
