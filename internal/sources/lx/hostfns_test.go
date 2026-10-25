package lx

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"testing"
)

func TestParseHeader(t *testing.T) {
	src := "/**\n * @name 测试源\n * @description 只是测试\n * @version 1.2.0\n * @author me\n * @homepage https://example.com\n */\nlx.send()"
	h := ParseHeader(src)
	if h.Name != "测试源" || h.Description != "只是测试" || h.Version != "1.2.0" || h.Author != "me" ||
		h.Homepage != "https://example.com" {
		t.Fatalf("%+v", h)
	}
	if ParseHeader("var a = 1").Name != "" {
		t.Fatal("expected empty header")
	}
}

func TestAES(t *testing.T) {
	key := []byte("0123456789abcdef")
	iv := []byte("fedcba9876543210")
	for _, mode := range []string{"aes-128-ecb", "aes-128-cbc"} {
		enc, err := aesCrypt(mode, []byte("hello lx"), key, iv, false)
		if err != nil || len(enc) != 16 {
			t.Fatalf("%s enc: %v %d", mode, err, len(enc))
		}
		dec, err := aesCrypt(mode, enc, key, iv, true)
		if err != nil || string(dec) != "hello lx" {
			t.Fatalf("%s dec: %v %q", mode, err, dec)
		}
	}
	// known vector: AES-128-ECB("abc", key "0123456789abcdef") with PKCS#7
	enc, _ := aesCrypt("aes-128-ecb", []byte("abc"), key, nil, false)
	if len(enc) != 16 {
		t.Fatal("padding")
	}
}

func TestRSARaw(t *testing.T) {
	k, _ := rsa.GenerateKey(rand.Reader, 1024)
	der, _ := x509.MarshalPKIXPublicKey(&k.PublicKey)
	p := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
	msg := append(make([]byte, 100), []byte("secret!")...)
	c, err := rsaRaw(msg, p)
	if err != nil || len(c) != 128 {
		t.Fatalf("%v %d", err, len(c))
	}
	m := new(big.Int).Exp(new(big.Int).SetBytes(c), k.D, k.N).Bytes()
	if !bytes.HasSuffix(m, []byte("secret!")) {
		t.Fatal("decrypt mismatch")
	}
}

func TestZlib(t *testing.T) {
	d, err := zlibOp("deflate", []byte("落雪落雪落雪"))
	if err != nil {
		t.Fatal(err)
	}
	i, err := zlibOp("inflate", d)
	if err != nil || string(i) != "落雪落雪落雪" {
		t.Fatalf("%v %q", err, i)
	}
}

func TestPickQuality(t *testing.T) {
	cases := []struct {
		decl []string
		want string
		got  string
	}{
		{[]string{"128k", "320k", "flac"}, "320k", "320k"},
		{[]string{"128k", "320k"}, "flac", "320k"},
		{[]string{"320k", "flac"}, "128k", "320k"},
		{nil, "320k", "320k"},
	}
	for _, c := range cases {
		if g := pickQuality(c.decl, c.want); g != c.got {
			t.Errorf("pickQuality(%v,%s)=%s want %s", c.decl, c.want, g, c.got)
		}
	}
}

func TestEapiParams(t *testing.T) {
	p, err := eapiParams("/api/cloudsearch/pc", map[string]any{"s": "x"})
	if err != nil || len(p)%32 != 0 || p != string(bytes.ToUpper([]byte(p))) {
		t.Fatalf("%v %q", err, p)
	}
}

func TestInterval(t *testing.T) {
	if interval(245) != "04:05" || interval(3725) != "1:02:05" || interval(0) != "" {
		t.Fatal(interval(245), interval(3725))
	}
}
