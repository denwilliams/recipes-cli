package netfetch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/denwilliams/recipes-cli/internal/fault"
)

const recipeURL = "https://www.recipetineats.com/recipe/"

var testHosts = []string{"www.recipetineats.com", "recipetineats.com"}
var public = netip.MustParseAddr("93.184.216.34")

func resolvePublic(context.Context, string) ([]netip.Addr, error) { return []netip.Addr{public}, nil }
func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	var e *fault.Error
	if !errors.As(err, &e) || e.Code != code {
		t.Fatalf("want %s, got %v", code, err)
	}
}

type trackedBody struct {
	io.Reader
	closes *int
}

func (b trackedBody) Close() error { (*b.closes)++; return nil }
func response(status int, headers http.Header, body string, closes *int) *http.Response {
	if headers == nil {
		headers = http.Header{"Content-Type": {"text/html; charset=UTF-8"}}
	}
	return &http.Response{StatusCode: status, Header: headers, Body: trackedBody{strings.NewReader(body), closes}, ContentLength: -1}
}
func loadPolicy(t *testing.T) (prefixes, good, bad []string) {
	t.Helper()
	b, err := os.ReadFile("testdata/iana-allocations.json")
	if err != nil {
		t.Fatal(err)
	}
	var data struct{ AllocatedPrefixes, PublicAddresses, ReservedAddresses []string }
	if err := json.Unmarshal(b, &data); err != nil {
		t.Fatal(err)
	}
	if len(data.AllocatedPrefixes) != 34 || len(data.PublicAddresses) != 68 || len(data.ReservedAddresses) != 32 {
		t.Fatal("unexpected IANA fixture")
	}
	return data.AllocatedPrefixes, data.PublicAddresses, data.ReservedAddresses
}
func TestPublicAddressPolicy(t *testing.T) {
	ps, good, bad := loadPolicy(t)
	actual := []string{}
	for _, p := range allocatedV6 {
		actual = append(actual, p.String())
	}
	if !reflect.DeepEqual(actual, ps) {
		t.Fatal("policy differs from independently captured IANA fixture")
	}
	good = append(good, "8.8.8.8", "93.184.216.34", "::ffff:8.8.8.8", "::ffff:808:808", "2001:4860:4860::8888", "2606:4700::1111", "2a00:1450:4001::1")
	for _, s := range good {
		t.Run("public/"+s, func(t *testing.T) {
			ip := netip.MustParseAddr(s)
			if !IsPublicAddress(ip) {
				t.Fatal("public refused")
			}
			opens, closes := 0, 0
			fetch := New(Options{Resolve: func(context.Context, string) ([]netip.Addr, error) { return []netip.Addr{ip}, nil }, Open: func(_ context.Context, u *url.URL, pinned netip.Addr) (*http.Response, error) {
				opens++
				if pinned != ip || u.String() != recipeURL {
					t.Fatal("pin/url changed")
				}
				return response(200, nil, "ok", &closes), nil
			}})
			page, err := fetch(context.Background(), recipeURL, testHosts)
			if err != nil || page.HTML != "ok" || opens != 1 || closes != 1 {
				t.Fatalf("%+v %v opens %d closes %d", page, err, opens, closes)
			}
		})
	}
	bad = append(bad, refusedSpecial...)
	for _, s := range bad {
		t.Run("refused/"+s, func(t *testing.T) {
			if a, err := netip.ParseAddr(s); err == nil && IsPublicAddress(a) {
				t.Fatal("non-public accepted")
			}
		})
	}
}

var refusedSpecial = []string{
	"64:ff9b:1::a00:1", "fec0::1", "3fff::1", "100:0:0:1::1", "5f00::1",
	"0.0.0.0", "10.0.0.1", "127.0.0.1", "100.64.0.1", "172.16.0.1", "192.168.1.1", "169.254.169.254",
	"192.0.0.9", "192.0.2.1", "192.31.196.1", "192.52.193.1", "192.88.99.1", "192.175.48.1", "198.18.0.1", "198.51.100.1", "203.0.113.1", "224.0.0.1", "240.0.0.1", "255.255.255.255",
	"::", "::1", "fc00::1", "fe80::1", "ff02::1", "2001:db8::1", "2001::1", "2001:1ff:ffff:ffff:ffff:ffff:ffff:ffff", "2002::1", "2620:4f:8000::1", "3ffe::1", "64:ff9b::808:808",
	"::ffff:10.0.0.1", "::ffff:127.0.0.1", "::ffff:192.168.1.1", "::ffff:169.254.169.254", "::ffff:192.0.2.1", "fe80::1%lo0", "not-ip", "010.0.0.1", "0x08080808", "134744072", "8.8.8",
}

func TestRejectedDNSZeroTransport(t *testing.T) {
	_, _, addresses := loadPolicy(t)
	addresses = append(addresses, refusedSpecial...)
	for _, s := range addresses {
		for _, mode := range []string{"initial", "mixed", "redirect", "redirect-mixed"} {
			t.Run(mode+"/"+s, func(t *testing.T) {
				// Invalid literals result in the invalid netip zero value: fail closed as well.
				ip, _ := netip.ParseAddr(s)
				dns, opens, closes := 0, 0, 0
				fetch := New(Options{Resolve: func(context.Context, string) ([]netip.Addr, error) {
					dns++
					if strings.HasPrefix(mode, "redirect") && dns == 1 {
						return []netip.Addr{public}, nil
					}
					if strings.Contains(mode, "mixed") {
						return []netip.Addr{public, ip}, nil
					}
					return []netip.Addr{ip}, nil
				}, Open: func(context.Context, *url.URL, netip.Addr) (*http.Response, error) {
					opens++
					return response(302, http.Header{"Location": {"/final/"}}, "", &closes), nil
				}})
				_, err := fetch(context.Background(), recipeURL, testHosts)
				wantCode(t, err, "NETWORK")
				want := 0
				if strings.HasPrefix(mode, "redirect") {
					want = 1
				}
				if opens != want || closes != want {
					t.Fatalf("transport %d closes %d, want %d (ZERO for rejected target)", opens, closes, want)
				}
			})
		}
	}
}
func TestURLBeforeDNS(t *testing.T) {
	bad := []string{"http://www.recipetineats.com/", "https://www.recipetineats.com:444/", "https://u:p@www.recipetineats.com/", "https://www.recipetineats.com.evil.example/", "https://127.0.0.1/", "file:///etc/passwd", "bad", "https://www.recipetineats.com./", "https://www.recipetineats.com:/", "https://www.recipetineats.com%00.evil/", "https://www.recipetineats.com\\@evil.example/"}
	for _, s := range bad {
		t.Run(s, func(t *testing.T) {
			dns, opens := 0, 0
			fetch := New(Options{Resolve: func(context.Context, string) ([]netip.Addr, error) { dns++; return []netip.Addr{public}, nil }, Open: func(context.Context, *url.URL, netip.Addr) (*http.Response, error) {
				opens++
				return nil, errors.New("forbidden")
			}})
			_, err := fetch(context.Background(), s, testHosts)
			wantCode(t, err, "URL")
			if dns != 0 || opens != 0 {
				t.Fatal("network reached")
			}
		})
	}
	for _, s := range []string{recipeURL + "#recipe", "https://WWW.RECIPETINEATS.COM:443/recipe/"} {
		u, err := ValidateURL(s, testHosts)
		if err != nil || u.String() != recipeURL {
			t.Fatalf("%v %v", u, err)
		}
	}
}
func TestEmptyDNS(t *testing.T) {
	opens := 0
	fetch := New(Options{Resolve: func(context.Context, string) ([]netip.Addr, error) { return nil, nil }, Open: func(context.Context, *url.URL, netip.Addr) (*http.Response, error) { opens++; return nil, nil }})
	_, err := fetch(context.Background(), recipeURL, testHosts)
	wantCode(t, err, "NETWORK")
	if opens != 0 {
		t.Fatal("transport reached")
	}
}
func TestRedirects(t *testing.T) {
	for _, status := range []int{301, 302, 303, 307, 308} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			dns, opens, closes := 0, 0, 0
			fetch := New(Options{Resolve: func(context.Context, string) ([]netip.Addr, error) { dns++; return []netip.Addr{public}, nil }, Open: func(_ context.Context, u *url.URL, _ netip.Addr) (*http.Response, error) {
				opens++
				if opens == 1 {
					return response(status, http.Header{"Location": {"/final/#ignored"}}, "", &closes), nil
				}
				if u.Path != "/final/" {
					t.Fatal(u)
				}
				return response(200, nil, "Riz 🍚", &closes), nil
			}})
			page, err := fetch(context.Background(), recipeURL, testHosts)
			if err != nil || page.URL != "https://www.recipetineats.com/final/" || page.HTML != "Riz 🍚" || opens != 2 || closes != 2 || dns != 2 {
				t.Fatalf("%+v %v %d %d %d", page, err, opens, closes, dns)
			}
		})
	}
	for _, target := range []string{"https://evil.example/", "http://www.recipetineats.com/", "https://127.0.0.1/", "https://u:p@www.recipetineats.com/", "https://www.recipetineats.com:444/"} {
		t.Run("refuse/"+target, func(t *testing.T) {
			dns, opens, closes := 0, 0, 0
			fetch := New(Options{Resolve: func(context.Context, string) ([]netip.Addr, error) { dns++; return []netip.Addr{public}, nil }, Open: func(context.Context, *url.URL, netip.Addr) (*http.Response, error) {
				opens++
				return response(302, http.Header{"Location": {target}}, "", &closes), nil
			}})
			_, err := fetch(context.Background(), recipeURL, testHosts)
			wantCode(t, err, "URL")
			if dns != 1 || opens != 1 || closes != 1 {
				t.Fatal("redirect reached network")
			}
		})
	}
	for _, headers := range []http.Header{{}, {"Location": {"https://["}}, {"Location": {"/one/", "/two/"}}, {"Location": {""}}} {
		closes := 0
		fetch := New(Options{Resolve: resolvePublic, Open: func(context.Context, *url.URL, netip.Addr) (*http.Response, error) {
			return response(302, headers, "", &closes), nil
		}})
		_, err := fetch(context.Background(), recipeURL, testHosts)
		wantCode(t, err, "REDIRECT")
		if closes != 1 {
			t.Fatal("unclosed")
		}
	}
	opens, closes := 0, 0
	fetch := New(Options{Resolve: resolvePublic, Open: func(context.Context, *url.URL, netip.Addr) (*http.Response, error) {
		opens++
		return response(302, http.Header{"Location": {"/loop/"}}, "", &closes), nil
	}})
	_, err := fetch(context.Background(), recipeURL, testHosts)
	wantCode(t, err, "REDIRECT")
	if opens != 4 || closes != 4 {
		t.Fatalf("default three-redirect bound %d/%d", opens, closes)
	}
}
func TestResponseFailuresAndLimits(t *testing.T) {
	cases := []struct {
		name, code string
		status     int
		headers    http.Header
		body       string
	}{
		{"403", "HTTP", 403, nil, ""}, {"404", "HTTP", 404, nil, ""}, {"429", "HTTP", 429, nil, ""}, {"500", "HTTP", 500, nil, ""}, {"204", "HTTP", 204, nil, ""},
		{"missing HTML", "HTTP", 200, http.Header{}, ""}, {"JSON", "HTTP", 200, http.Header{"Content-Type": {"application/json"}}, ""},
		{"compression", "HTTP", 200, http.Header{"Content-Type": {"text/html"}, "Content-Encoding": {"gzip"}}, ""},
		{"charset", "HTTP", 200, http.Header{"Content-Type": {"text/html; charset=iso-8859-1"}}, ""},
		{"declared size", "SIZE", 200, http.Header{"Content-Type": {"text/html"}, "Content-Length": {"6"}}, "123"},
		{"stream size", "SIZE", 200, nil, "123456"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			closes := 0
			fetch := New(Options{MaxBytes: 5, Resolve: resolvePublic, Open: func(context.Context, *url.URL, netip.Addr) (*http.Response, error) {
				return response(c.status, c.headers, c.body, &closes), nil
			}})
			_, err := fetch(context.Background(), recipeURL, testHosts)
			wantCode(t, err, c.code)
			if closes != 1 {
				t.Fatal("unclosed")
			}
		})
	}
	closes := 0
	fetch := New(Options{MaxBytes: 5, Resolve: resolvePublic, Open: func(context.Context, *url.URL, netip.Addr) (*http.Response, error) {
		return response(200, nil, "12345", &closes), nil
	}})
	page, err := fetch(context.Background(), recipeURL, testHosts)
	if err != nil || page.HTML != "12345" {
		t.Fatal(page, err)
	}
}

type failReader struct{}

func (failReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func TestNetworkFailures(t *testing.T) {
	for _, stage := range []string{"dns", "headers", "body"} {
		t.Run(stage, func(t *testing.T) {
			closes := 0
			fetch := New(Options{Resolve: func(ctx context.Context, host string) ([]netip.Addr, error) {
				if stage == "dns" {
					return nil, errors.New("DNS")
				}
				return resolvePublic(ctx, host)
			}, Open: func(context.Context, *url.URL, netip.Addr) (*http.Response, error) {
				if stage == "headers" {
					return nil, errors.New("TLS")
				}
				res := response(200, nil, "", &closes)
				res.Body = trackedBody{failReader{}, &closes}
				return res, nil
			}})
			_, err := fetch(context.Background(), recipeURL, testHosts)
			wantCode(t, err, "NETWORK")
			if stage == "body" && closes != 1 {
				t.Fatal("unclosed")
			}
		})
	}
}

type contextReader struct{ ctx context.Context }

func (r contextReader) Read([]byte) (int, error) { <-r.ctx.Done(); return 0, r.ctx.Err() }
func TestSingleDeadline(t *testing.T) {
	for _, stage := range []string{"dns", "headers", "body", "redirects", "late-dns", "late-headers"} {
		t.Run(stage, func(t *testing.T) {
			opens, closes, dns := 0, 0, 0
			var deadline time.Time
			fetch := New(Options{Timeout: 100 * time.Millisecond, Resolve: func(ctx context.Context, _ string) ([]netip.Addr, error) {
				dns++
				current, ok := ctx.Deadline()
				if !ok {
					t.Fatal("no deadline")
				}
				if dns == 1 {
					deadline = current
				} else if !deadline.Equal(current) {
					t.Fatal("deadline reset across redirects")
				}
				if stage == "dns" {
					<-ctx.Done()
					return nil, ctx.Err()
				}
				if stage == "late-dns" {
					<-ctx.Done()
					return []netip.Addr{public}, nil
				}
				return []netip.Addr{public}, nil
			}, Open: func(ctx context.Context, _ *url.URL, _ netip.Addr) (*http.Response, error) {
				opens++
				if stage == "headers" {
					<-ctx.Done()
					return nil, ctx.Err()
				}
				if stage == "late-headers" {
					<-ctx.Done()
					return response(200, nil, "late", &closes), nil
				}
				res := response(200, nil, "", &closes)
				if stage == "body" {
					res.Body = trackedBody{contextReader{ctx}, &closes}
				}
				if stage == "redirects" {
					if opens == 2 {
						<-ctx.Done()
						return nil, ctx.Err()
					}
					res.StatusCode = 302
					res.Header.Set("Location", "/again/")
				}
				return res, nil
			}})
			_, err := fetch(context.Background(), recipeURL, testHosts)
			wantCode(t, err, "TIMEOUT")
			if (stage == "dns" || stage == "late-dns") && opens != 0 {
				t.Fatal("late DNS reached transport")
			}
			if (stage == "body" || stage == "late-headers") && closes != 1 {
				t.Fatal("late response unclosed")
			}
			if stage == "redirects" && (dns != 2 || opens != 2) {
				t.Fatalf("deadline not aggregate: dns%d opens%d", dns, opens)
			}
		})
	}
}
func TestPinnedNativeTransportNoReresolution(t *testing.T) {
	u, _ := url.Parse(recipeURL)
	for _, s := range []string{"93.184.216.34", "2606:4700::1111", "::ffff:8.8.8.8"} {
		t.Run(s, func(t *testing.T) {
			ip := netip.MustParseAddr(s)
			calls := 0
			sentinel := errors.New("no socket allowed")
			tr := pinnedTransport(u, ip, func(_ context.Context, network, address string) (net.Conn, error) {
				calls++
				if network != "tcp" || address != net.JoinHostPort(ip.Unmap().String(), "443") {
					t.Fatalf("dial %s %s", network, address)
				}
				return nil, sentinel
			})
			if tr.Proxy != nil || !tr.DisableKeepAlives || !tr.DisableCompression || tr.TLSClientConfig.InsecureSkipVerify || tr.TLSClientConfig.ServerName != u.Hostname() {
				t.Fatal("unsafe transport defaults")
			}
			req, _ := http.NewRequest("GET", recipeURL, nil)
			_, err := tr.RoundTrip(req)
			if !errors.Is(err, sentinel) || calls != 1 {
				t.Fatalf("%v calls%d", err, calls)
			}
			// The adapter checks target identity and cancellation again immediately before dial.
			_, err = tr.DialContext(context.Background(), "tcp", "evil.example:443")
			if err == nil || calls != 1 {
				t.Fatal("target bypass")
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			_, err = tr.DialContext(ctx, "tcp", "www.recipetineats.com:443")
			if err == nil || calls != 1 {
				t.Fatal("late dial")
			}
		})
	}
	calls := 0
	tr := pinnedTransport(u, netip.MustParseAddr("127.0.0.1"), func(context.Context, string, string) (net.Conn, error) { calls++; return nil, nil })
	_, err := tr.DialContext(context.Background(), "tcp", "www.recipetineats.com:443")
	if err == nil || calls != 0 {
		t.Fatal("unsafe pin reached dial")
	}
}
