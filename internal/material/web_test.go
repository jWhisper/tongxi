package material

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(r *http.Request, status int, kind, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {kind}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}
}
func TestPublicWebAddressGuards(t *testing.T) {
	for _, s := range []string{"http://localhost/a", "http://127.0.0.1/", "http://[::1]", "http://10.0.0.2", "http://169.254.169.254/latest", "https://example.com:8080/", "file:///etc/passwd", "https://user:pass@example.com/", "http://printer.local/"} {
		if _, err := publicURL(s); err == nil {
			t.Fatal("accepted", s)
		}
	}
	for _, ip := range []string{"127.0.0.1", "10.1.1.1", "172.16.0.1", "192.168.1.1", "169.254.1.1", "100.64.1.1", "::1", "fc00::1", "::ffff:127.0.0.1", "0.0.0.0", "224.0.0.1"} {
		if publicIP(netip.MustParseAddr(ip)) {
			t.Fatal("unsafe IP", ip)
		}
	}
	if !publicIP(netip.MustParseAddr("1.1.1.1")) {
		t.Fatal("public address blocked")
	}
	w := NewWeb()
	calls := 0
	w.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		resp := response(r, 302, "text/html", "")
		resp.Header.Set("Location", "http://127.0.0.1/private")
		return resp, nil
	})
	if _, _, _, err := w.fetch(context.Background(), "https://example.com"); err == nil || calls != 1 {
		t.Fatal("private redirect followed", calls, err)
	}
}
func TestSearchAndWebSnapshotsAndErrors(t *testing.T) {
	w := NewWeb()
	w.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/search" {
			if r.URL.Query().Get("q") != "读书会" || r.URL.Query().Get("format") != "rss" {
				t.Fatal(r.URL)
			}
			return response(r, 200, "application/rss+xml", `<rss><channel><item><title>活动资料</title><link>https://example.com/page</link><description>只作搜索线索</description></item><item><title>内网</title><link>http://127.0.0.1</link></item></channel></rss>`), nil
		}
		switch r.URL.Path {
		case "/missing":
			return response(r, 404, "text/html", "no"), nil
		case "/large":
			return response(r, 200, "text/html", strings.Repeat("x", (5<<20)+1)), nil
		case "/empty":
			return response(r, 200, "text/html", "<html><body><script>content()</script></body></html>"), nil
		}
		return response(r, 200, "text/html; charset=utf-8", "<html><head><title>活动正文</title></head><body><article><h1>活动正文</h1><p>"+strings.Repeat("报名人数为12人，预算上限100元。", 40)+"</p><script>do_not_keep()</script></article></body></html>"), nil
	})
	hits, err := w.Search(context.Background(), "读书会")
	if err != nil || len(hits) != 1 {
		t.Fatal(hits, err)
	}
	d, url, raw, err := w.Read(context.Background(), hits[0].URL)
	if err != nil || url != hits[0].URL || len(raw) == 0 || len(d.Segments) == 0 {
		t.Fatal(d, url, err)
	}
	if !strings.Contains(d.Segments[0].Content, "活动正文") || strings.Contains(fmt.Sprint(d.Segments), "do_not_keep") {
		t.Fatal(d)
	}
	for _, path := range []string{"missing", "large", "empty"} {
		if _, _, _, err := w.Read(context.Background(), "https://example.com/"+path); err == nil {
			t.Fatal("accepted", path)
		}
	}
}
func TestLivePublicWeb(t *testing.T) {
	if os.Getenv("TONGXI_WEB_SMOKE") != "1" {
		t.Skip("explicit live network check")
	}
	w := NewWeb()
	hits, err := w.Search(context.Background(), "Golang official documentation")
	if err != nil || len(hits) == 0 {
		t.Fatal(hits, err)
	}
	doc, url, _, err := w.Read(context.Background(), "https://go.dev/doc/")
	if err != nil || len(doc.Segments) == 0 {
		t.Fatal(err)
	}
	t.Logf("search results=%d; read %s, %d segments", len(hits), url, len(doc.Segments))
}
