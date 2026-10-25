package netguard

import (
	"io"
	"net"
	"testing"
)

func TestAllowed(t *testing.T) {
	for ip, want := range map[string]bool{
		"8.8.8.8": true, "1.1.1.1": true, "2606:4700::1111": true,
		"127.0.0.1": false, "10.1.2.3": false, "172.16.0.1": false, "192.168.1.1": false,
		"169.254.169.254": false, "100.100.100.200": false, "0.0.0.0": false, "::1": false,
		"fe80::1": false, "fd00::1": false, "::ffff:127.0.0.1": false,
	} {
		if got := Allowed(net.ParseIP(ip)); got != want {
			t.Errorf("Allowed(%s) = %v, want %v", ip, got, want)
		}
	}
}

func TestCheckURL(t *testing.T) {
	for raw, ok := range map[string]bool{
		"https://example.com/a.mp3": true, "http://radio.example.com:8000/": true,
		"ftp://x/y": false, "file:///etc/passwd": false, "http://127.0.0.1/": false,
		"http://[::1]/": false, "https:///nohost": false, "javascript:alert(1)": false,
	} {
		if _, err := CheckURL(raw); (err == nil) != ok {
			t.Errorf("CheckURL(%q) err=%v, want ok=%v", raw, err, ok)
		}
	}
}

func TestICYRewrite(t *testing.T) {
	a, b := net.Pipe()
	go func() {
		a.Write([]byte("ICY 200 OK\r\nicy-name: x\r\n\r\n"))
		a.Close()
	}()
	got, _ := io.ReadAll(&icyConn{Conn: b})
	if string(got) != "HTTP/1.0 200 OK\r\nicy-name: x\r\n\r\n" {
		t.Fatalf("got %q", got)
	}

	c, d := net.Pipe()
	go func() {
		c.Write([]byte("HTTP/1.1 200 OK\r\n\r\n"))
		c.Close()
	}()
	got, _ = io.ReadAll(&icyConn{Conn: d})
	if string(got) != "HTTP/1.1 200 OK\r\n\r\n" {
		t.Fatalf("got %q", got)
	}
}
