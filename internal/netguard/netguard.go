// Package netguard builds outbound HTTP clients that are safe to point at
// user-supplied URLs (direct links, radio streams, relays, LX scripts).
//
// The server must never become a door into its own network (SSRF), so the
// dialer refuses loopback, private, link-local, CGNAT and other special
// addresses — checked on the resolved IP of every connection, including
// each redirect hop. On Aliyun ECS this also blocks the instance metadata
// service at 100.100.100.200.
//
// The client also understands old SHOUTcast servers that answer
// "ICY 200 OK" instead of "HTTP/1.0 200 OK".
package netguard

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"
)

// ErrBlocked is returned when a destination address is not allowed.
var ErrBlocked = errors.New("destination address not allowed")

// UserAgent is a browser-like UA; many stream hosts reject unknown clients.
const UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 " +
	"(KHTML, like Gecko) Chrome/124.0 Safari/537.36"

var specialNets = func() []*net.IPNet {
	var out []*net.IPNet
	for _, c := range []string{
		"0.0.0.0/8",       // "this" network
		"100.64.0.0/10",   // CGNAT, incl. Aliyun metadata 100.100.100.200
		"192.0.0.0/24",    // IETF protocol assignments
		"192.0.2.0/24",    // TEST-NET-1
		"198.18.0.0/15",   // benchmarking
		"198.51.100.0/24", // TEST-NET-2
		"203.0.113.0/24",  // TEST-NET-3
		"240.0.0.0/4",     // reserved
		"64:ff9b::/96",    // NAT64 (could map to private v4)
		"2001:db8::/32",   // documentation
	} {
		_, n, err := net.ParseCIDR(c)
		if err == nil {
			out = append(out, n)
		}
	}
	return out
}()

// Allowed reports whether ip is a public unicast address.
func Allowed(ip net.IP) bool {
	if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() || ip.IsMulticast() ||
		ip.IsUnspecified() {
		return false
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	for _, n := range specialNets {
		if n.Contains(ip) {
			return false
		}
	}
	return true
}

func control(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	if !Allowed(net.ParseIP(host)) {
		return fmt.Errorf("%w: %s", ErrBlocked, host)
	}
	return nil
}

// Options configures a client.
type Options struct {
	// AllowPrivate disables the address check (desktop / LAN use only).
	AllowPrivate bool
	// Timeout is the whole-request timeout; 0 for streams.
	Timeout time.Duration
	// HeaderTimeout bounds the wait for response headers (default 15s).
	HeaderTimeout time.Duration
}

// NewClient returns an HTTP client with the SSRF guard and ICY support.
func NewClient(o Options) *http.Client {
	if o.HeaderTimeout <= 0 {
		o.HeaderTimeout = 15 * time.Second
	}
	d := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	if !o.AllowPrivate {
		d.Control = control
	}
	tr := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			c, err := d.DialContext(ctx, network, addr)
			if err != nil {
				return nil, err
			}
			return &icyConn{Conn: c}, nil
		},
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          32,
		MaxIdleConnsPerHost:   4,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: o.HeaderTimeout,
		ExpectContinueTimeout: time.Second,
	}
	return &http.Client{
		Transport: tr,
		Timeout:   o.Timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 6 {
				return errors.New("too many redirects")
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return errors.New("redirect to unsupported scheme")
			}
			return nil
		},
	}
}

// CheckURL validates a user-supplied URL (http/https with a host).
func CheckURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if len(raw) > 4096 {
		return nil, errors.New("URL too long")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, errors.New("invalid URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, errors.New("only http and https URLs are supported")
	}
	if u.Hostname() == "" {
		return nil, errors.New("URL has no host")
	}
	// literal IPs are checked again at dial time; this gives a nicer error
	if ip := net.ParseIP(u.Hostname()); ip != nil && !Allowed(ip) {
		return nil, ErrBlocked
	}
	return u, nil
}

// icyConn rewrites a leading "ICY " status line to "HTTP/1.0 " so net/http
// can parse SHOUTcast v1 responses. TLS traffic never starts with "ICY ",
// so wrapping every connection is harmless.
type icyConn struct {
	net.Conn
	checked bool
	pending []byte
}

func (c *icyConn) Read(p []byte) (int, error) {
	if len(c.pending) > 0 {
		n := copy(p, c.pending)
		c.pending = c.pending[n:]
		return n, nil
	}
	if c.checked {
		return c.Conn.Read(p)
	}
	c.checked = true
	head := make([]byte, 4)
	n, err := io.ReadFull(c.Conn, head)
	if n == 4 && string(head) == "ICY " {
		c.pending = []byte("HTTP/1.0 ")
	} else {
		c.pending = head[:n]
	}
	if len(c.pending) == 0 {
		return 0, err
	}
	m := copy(p, c.pending)
	c.pending = c.pending[m:]
	return m, nil
}
