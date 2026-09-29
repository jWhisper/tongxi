package material

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	readability "codeberg.org/readeck/go-readability/v2"
	"golang.org/x/net/html/charset"
)

type Web struct {
	client    *http.Client
	searchURL string
}
type SearchHit struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

func NewWeb() *Web {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = nil
	tr.DialContext = publicDial
	tr.ResponseHeaderTimeout = 12 * time.Second
	tr.MaxResponseHeaderBytes = 1 << 20
	return &Web{client: &http.Client{Transport: tr, Timeout: 20 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("网页重定向次数过多")
		}
		_, err := publicURL(r.URL.String())
		return err
	}}, searchURL: "https://www.bing.com/search"}
}
func publicIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, prefix := range []string{"100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001:db8::/32"} {
		if netip.MustParsePrefix(prefix).Contains(ip) {
			return false
		}
	}
	return true
}
func publicURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") || len(raw) > 4096 {
		return nil, errors.New("请使用不含账号密码的公开 HTTP(S) 网页链接")
	}
	if port := u.Port(); port != "" && port != "80" && port != "443" {
		return nil, errors.New("网页读取仅支持80或443端口")
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") {
		return nil, errors.New("网页读取不访问本机或内网地址")
	}
	if ip, err := netip.ParseAddr(host); err == nil && !publicIP(ip) {
		return nil, errors.New("网页读取不访问本机或内网地址")
	}
	u.Fragment = ""
	return u, nil
}
func publicDial(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, errors.New("网页域名解析失败")
	}
	if len(ips) == 0 {
		return nil, errors.New("网页域名没有可用地址")
	}
	// Some local VPNs return benchmarking-range synthetic addresses. Resolve
	// those names via a fixed HTTPS resolver, then still dial a validated public
	// IP. Never relax the private-address guard for the synthetic destination.
	fakeRange := netip.MustParsePrefix("198.18.0.0/15")
	for _, ip := range ips {
		if fakeRange.Contains(ip.Unmap()) {
			ips, err = resolvePublicDNS(ctx, host)
			if err != nil {
				return nil, err
			}
			break
		}
	}
	for _, ip := range ips {
		if !publicIP(ip) {
			return nil, errors.New("网页域名指向本机或内网，已拒绝访问")
		}
	}
	// Dial the validated IP itself; no second DNS resolution can change the target.
	var last error
	for _, ip := range ips {
		conn, e := (&net.Dialer{Timeout: 8 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if e == nil {
			return conn, nil
		}
		last = e
	}
	return nil, last
}

func resolvePublicDNS(ctx context.Context, host string) ([]netip.Addr, error) {
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", "https://dns.alidns.com/resolve?type=A&name="+url.QueryEscape(host), nil)
	if err != nil {
		return nil, err
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = nil
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("当前网络使用虚拟 DNS，公网域名解析失败")
	}
	defer resp.Body.Close()
	var answer struct {
		Status int
		Answer []struct {
			Type int
			Data string
		}
	}
	if resp.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp.Body, 32768)).Decode(&answer) != nil || answer.Status != 0 {
		return nil, errors.New("公网 DNS 未返回可用地址")
	}
	ips := []netip.Addr{}
	for _, record := range answer.Answer {
		if record.Type != 1 {
			continue
		}
		ip, err := netip.ParseAddr(record.Data)
		if err != nil || !publicIP(ip) {
			return nil, errors.New("公网 DNS 返回了不可访问的地址")
		}
		ips = append(ips, ip)
	}
	if len(ips) == 0 {
		return nil, errors.New("公网 DNS 未返回可用地址")
	}
	return ips, nil
}
func (w *Web) fetch(ctx context.Context, raw string) ([]byte, *url.URL, string, error) {
	u, err := publicURL(raw)
	if err != nil {
		return nil, nil, "", err
	}
	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, nil, "", err
	}
	req.Header.Set("User-Agent", "Tongxi/0.1.17 (public document reader)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/pdf,text/plain,application/rss+xml,application/xml")
	resp, err := w.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, nil, "", ctx.Err()
		}
		return nil, nil, "", errors.New("网页连接失败或超时，请检查链接或换一个来源")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, nil, "", fmt.Errorf("网页返回 HTTP %d，未获得正文", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (5<<20)+1))
	if err != nil {
		return nil, nil, "", errors.New("网页下载不完整，请重试")
	}
	if len(data) > 5<<20 {
		return nil, nil, "", errors.New("网页响应超过5MB，请换用较小页面或下载后导入")
	}
	return data, resp.Request.URL, resp.Header.Get("Content-Type"), nil
}
func (w *Web) Search(ctx context.Context, query string) ([]SearchHit, error) {
	query = strings.TrimSpace(query)
	if query == "" || len([]rune(query)) > 300 {
		return nil, errors.New("搜索词请输入1–300个字符")
	}
	u, _ := url.Parse(w.searchURL)
	q := u.Query()
	q.Set("q", query)
	q.Set("format", "rss")
	u.RawQuery = q.Encode()
	data, _, _, err := w.fetch(ctx, u.String())
	if err != nil {
		return nil, err
	}
	var feed struct {
		Channel struct {
			Items []struct {
				Title       string `xml:"title"`
				Link        string `xml:"link"`
				Description string `xml:"description"`
			} `xml:"item"`
		} `xml:"channel"`
	}
	if err = xml.Unmarshal(data, &feed); err != nil {
		return nil, errors.New("公开搜索暂不可用，请改用直接网页链接")
	}
	out := []SearchHit{}
	seen := map[string]bool{}
	for _, item := range feed.Channel.Items {
		link, e := publicURL(item.Link)
		if e != nil || seen[link.String()] {
			continue
		}
		seen[link.String()] = true
		out = append(out, SearchHit{Title: trimChars(item.Title, 200), URL: link.String(), Snippet: trimChars(item.Description, 400)})
		if len(out) == 6 {
			break
		}
	}
	if len(out) == 0 {
		return nil, errors.New("没有获得公开搜索结果，请换关键词或提供直接链接")
	}
	return out, nil
}
func (w *Web) Read(ctx context.Context, raw string) (Document, string, []byte, error) {
	data, u, kind, err := w.fetch(ctx, raw)
	if err != nil {
		return Document{}, "", nil, err
	}
	if strings.Contains(kind, "application/pdf") || bytes.HasPrefix(data, []byte("%PDF-")) {
		doc, e := Parse(ctx, "网页.pdf", data)
		return doc, u.String(), data, e
	}
	doc := Document{Name: u.Hostname(), Format: "html", Note: "保存读取时的网页正文快照；不执行脚本，不登录，不加载图片。读取时间不等于文章发布时间。"}
	if strings.Contains(kind, "text/plain") {
		text, e := decodeText(data, false)
		if e != nil {
			return doc, "", nil, e
		}
		doc.Format = "txt"
		for i, line := range strings.Split(text, "\n") {
			if err = doc.add(fmt.Sprintf("第%d行", i+1), line); err != nil {
				return doc, "", nil, err
			}
		}
	} else {
		if kind != "" && !strings.Contains(kind, "html") {
			return doc, "", nil, errors.New("该链接未返回 HTML、纯文本或 PDF 正文")
		}
		reader, e := charset.NewReader(bytes.NewReader(data), kind)
		if e != nil {
			return doc, "", nil, e
		}
		article, e := readability.FromReader(reader, u)
		if e != nil || article.Node == nil {
			return doc, "", nil, errors.New("未找到可读取正文，页面可能需要登录或执行脚本")
		}
		var b strings.Builder
		if e = article.RenderText(&b); e != nil {
			return doc, "", nil, e
		}
		title := trimChars(strings.TrimSpace(article.Title()), 200)
		if title != "" {
			doc.Name = title
		}
		for i, part := range strings.Split(b.String(), "\n") {
			if err = doc.add(fmt.Sprintf("正文第%d段", i+1), part); err != nil {
				return doc, "", nil, err
			}
		}
	}
	if len(doc.Segments) == 0 {
		return doc, "", nil, errors.New("网页没有可引用的文字正文")
	}
	return doc, u.String(), data, nil
}
func trimChars(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
