// Package netfetch implements bounded, public-address-pinned provider HTTPS.
package netfetch

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/denwilliams/recipes-cli/internal/fault"
)

type Page struct{ HTML, URL string }
type Fetch func(context.Context, string, []string) (Page, error)
type Resolve func(context.Context, string) ([]netip.Addr, error)
type Open func(context.Context, *url.URL, netip.Addr) (*http.Response, error)
type Options struct {
	Timeout      time.Duration
	MaxBytes     int64
	MaxRedirects int // zero uses the default (3)
	Resolve      Resolve
	Open         Open
}

func ValidateURL(input string, hosts []string) (*url.URL, error) {
	u, err := url.Parse(input)
	if err != nil || u.Scheme != "https" || u.Opaque != "" || u.User != nil || u.Host == "" || (u.Port() != "" && u.Port() != "443") {
		return nil, fault.New("URL", "Use a full HTTPS URL on a supported provider host, without credentials or custom port.")
	}
	// Lowercase DNS is case-insensitive; no trailing dots/escaped/deceptive authorities.
	host := strings.ToLower(u.Hostname())
	valid := false
	for _, h := range hosts {
		if host == h {
			valid = true
		}
	}
	if !valid || (strings.ToLower(u.Host) != host && strings.ToLower(u.Host) != host+":443") {
		return nil, fault.New("URL", "Unsupported provider host. Run recipes providers status for available providers.")
	}
	u.Host = host
	u.Fragment = ""
	u.RawFragment = ""
	if u.Path == "" {
		u.Path = "/"
	}
	return u, nil
}

// pinnedTransport never receives an unvalidated hostname for dialing. The URL/Host
// and TLS ServerName remain the original provider hostname; certificate verification
// is enabled. No proxy, DNS re-resolution, connection reuse, HTTP/2 coalescing or retries.
func pinnedTransport(u *url.URL, ip netip.Addr, dial func(context.Context, string, string) (net.Conn, error)) *http.Transport {
	return &http.Transport{
		Proxy: nil, DisableCompression: true, DisableKeepAlives: true,
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, ServerName: u.Hostname()},
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if !IsPublicAddress(ip) || address != net.JoinHostPort(u.Hostname(), "443") {
				return nil, errors.New("unvalidated transport target")
			}
			return dial(ctx, "tcp", net.JoinHostPort(ip.Unmap().String(), "443"))
		},
	}
}

type transportBody struct {
	io.ReadCloser
	transport *http.Transport
}

func (b *transportBody) Close() error {
	err := b.ReadCloser.Close()
	b.transport.CloseIdleConnections()
	return err
}
func openHTTPS(ctx context.Context, u *url.URL, ip netip.Addr) (*http.Response, error) {
	transport := pinnedTransport(u, ip, (&net.Dialer{}).DialContext)
	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "recipes-cli/0.1.0-go (+https://github.com/denwilliams/recipes-cli)")
	req.Header.Set("Accept", "text/html")
	req.Header.Set("Accept-Encoding", "identity")
	res, err := transport.RoundTrip(req) // no automatic redirect path exists
	if err != nil {
		transport.CloseIdleConnections()
		return nil, err
	}
	res.Body = &transportBody{res.Body, transport}
	return res, nil
}
func New(opts Options) Fetch {
	if opts.Timeout <= 0 {
		opts.Timeout = 10 * time.Second
	}
	if opts.MaxBytes <= 0 {
		opts.MaxBytes = 2 * 1024 * 1024
	}
	if opts.MaxRedirects <= 0 {
		opts.MaxRedirects = 3
	}
	if opts.Resolve == nil {
		opts.Resolve = func(ctx context.Context, host string) ([]netip.Addr, error) {
			return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		}
	}
	if opts.Open == nil {
		opts.Open = openHTTPS
	}
	return func(parent context.Context, input string, hosts []string) (page Page, err error) {
		ctx, cancel := context.WithTimeout(parent, opts.Timeout)
		defer cancel()
		defer func() {
			if err == nil {
				return
			}
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				err = fault.New("TIMEOUT", "Request timed out after %s. Try again later.", opts.Timeout)
				return
			}
			var known *fault.Error
			if !errors.As(err, &known) {
				err = fault.New("NETWORK", "Could not fetch provider page. Check DNS, connection and TLS; try again later.")
			}
		}()
		u, err := ValidateURL(input, hosts)
		if err != nil {
			return Page{}, err
		}
		for redirects := 0; ; redirects++ {
			ips, err := opts.Resolve(ctx, u.Hostname())
			if err != nil {
				return Page{}, err
			}
			if err := ctx.Err(); err != nil {
				return Page{}, err
			}
			if len(ips) == 0 {
				return Page{}, fault.New("NETWORK", "Provider DNS returned no public addresses; request refused.")
			}
			for _, ip := range ips {
				if !IsPublicAddress(ip) {
					return Page{}, fault.New("NETWORK", "Provider DNS did not resolve exclusively to public addresses; request refused.")
				}
			}
			res, err := opts.Open(ctx, u, ips[0])
			if err != nil {
				return Page{}, err
			}
			if res == nil || res.Body == nil {
				return Page{}, fault.New("NETWORK", "Provider returned an invalid response.")
			}
			next, html, err := readResponse(ctx, res, u, hosts, redirects, opts)
			res.Body.Close()
			if err != nil {
				return Page{}, err
			}
			if next != nil {
				u = next
				continue
			}
			return Page{html, u.String()}, nil
		}
	}
}
func readResponse(ctx context.Context, res *http.Response, u *url.URL, hosts []string, redirects int, opts Options) (*url.URL, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	switch res.StatusCode {
	case 301, 302, 303, 307, 308:
		if redirects >= opts.MaxRedirects {
			return nil, "", fault.New("REDIRECT", "Too many redirects (maximum %d). Use final supported URL.", opts.MaxRedirects)
		}
		locations := res.Header.Values("Location")
		if len(locations) != 1 || strings.TrimSpace(locations[0]) == "" {
			return nil, "", fault.New("REDIRECT", "Redirect has no single valid Location header.")
		}
		ref, err := url.Parse(locations[0])
		if err != nil {
			return nil, "", fault.New("REDIRECT", "Redirect contains invalid URL.")
		}
		next, err := ValidateURL(u.ResolveReference(ref).String(), hosts)
		return next, "", err
	}
	if res.StatusCode != 200 {
		return nil, "", fault.New("HTTP", "Provider returned HTTP %d. Check URL or try later; restrictions are not bypassed.", res.StatusCode)
	}
	media, params, err := mime.ParseMediaType(res.Header.Get("Content-Type"))
	if err != nil || (media != "text/html" && media != "application/xhtml+xml") {
		return nil, "", fault.New("HTTP", "Provider did not return HTML. Use a recipe page.")
	}
	if charset := strings.ToLower(params["charset"]); charset != "" && charset != "utf-8" {
		return nil, "", fault.New("HTTP", "Only UTF-8 HTML is supported.")
	}
	if enc := strings.ToLower(res.Header.Get("Content-Encoding")); enc != "" && enc != "identity" {
		return nil, "", fault.New("HTTP", "Compressed responses unsupported; provider ignored identity encoding.")
	}
	declared, _ := strconv.ParseInt(res.Header.Get("Content-Length"), 10, 64)
	if res.ContentLength > opts.MaxBytes || declared > opts.MaxBytes {
		return nil, "", fault.New("SIZE", "Response exceeds %d-byte limit.", opts.MaxBytes)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, opts.MaxBytes+1))
	if err != nil {
		return nil, "", err
	}
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	if int64(len(b)) > opts.MaxBytes {
		return nil, "", fault.New("SIZE", "Response exceeds %d-byte limit.", opts.MaxBytes)
	}
	return nil, string(b), nil
}
