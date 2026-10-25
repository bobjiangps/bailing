package lx

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rsa"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"mime/multipart"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"bailing/internal/netguard"
)

// ---- script header ---------------------------------------------------------------

// ScriptInfo is parsed from the leading comment block of a script:
//
//	/**
//	 * @name Example source
//	 * @description ...
//	 * @version 1.0.0
//	 * @author someone
//	 * @homepage https://...
//	 */
type ScriptInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     string `json:"version"`
	Author      string `json:"author"`
	Homepage    string `json:"homepage"`
	RawScript   string `json:"rawScript,omitempty"`
}

var reTag = regexp.MustCompile(`^\s*\*?\s*@(\w+)\s+(.*?)\s*$`)

// ParseHeader reads @name, @description … from the first /* */ comment.
func ParseHeader(src string) ScriptInfo {
	var info ScriptInfo
	s := strings.TrimLeft(src, "\ufeff \t\r\n")
	if !strings.HasPrefix(s, "/*") {
		return info
	}
	end := strings.Index(s, "*/")
	if end < 0 {
		return info
	}
	for _, ln := range strings.Split(s[2:end], "\n") {
		m := reTag.FindStringSubmatch(ln)
		if m == nil {
			continue
		}
		v := clip(m[2], 300)
		switch strings.ToLower(m[1]) {
		case "name":
			info.Name = clip(v, 40)
		case "description":
			info.Description = v
		case "version":
			info.Version = clip(v, 40)
		case "author":
			info.Author = clip(v, 60)
		case "homepage":
			info.Homepage = v
		}
	}
	return info
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

// ---- crypto helpers (lx.utils.crypto) ----------------------------------------------

func md5Hex(data []byte) string {
	sum := md5.Sum(data)
	return hex.EncodeToString(sum[:])
}

// aesCrypt implements aes-128/192/256 in cbc or ecb mode with PKCS#7
// padding (Node's createCipheriv defaults), which is what LX sources use.
func aesCrypt(mode string, data, key, iv []byte, decrypt bool) ([]byte, error) {
	mode = strings.ToLower(mode)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	bs := block.BlockSize()
	switch {
	case strings.HasSuffix(mode, "-ecb"):
		if decrypt {
			if len(data)%bs != 0 {
				return nil, errors.New("aes: input not a multiple of block size")
			}
			out := make([]byte, len(data))
			for i := 0; i < len(data); i += bs {
				block.Decrypt(out[i:i+bs], data[i:i+bs])
			}
			return unpad(out, bs)
		}
		in := pad(data, bs)
		out := make([]byte, len(in))
		for i := 0; i < len(in); i += bs {
			block.Encrypt(out[i:i+bs], in[i:i+bs])
		}
		return out, nil
	case strings.HasSuffix(mode, "-cbc"):
		if len(iv) != bs {
			return nil, errors.New("aes-cbc: iv must be 16 bytes")
		}
		if decrypt {
			if len(data)%bs != 0 {
				return nil, errors.New("aes: input not a multiple of block size")
			}
			out := make([]byte, len(data))
			cipher.NewCBCDecrypter(block, iv).CryptBlocks(out, data)
			return unpad(out, bs)
		}
		in := pad(data, bs)
		out := make([]byte, len(in))
		cipher.NewCBCEncrypter(block, iv).CryptBlocks(out, in)
		return out, nil
	}
	return nil, fmt.Errorf("unsupported cipher mode %q", mode)
}

func pad(b []byte, bs int) []byte {
	n := bs - len(b)%bs
	return append(append([]byte{}, b...), bytes.Repeat([]byte{byte(n)}, n)...)
}

func unpad(b []byte, bs int) ([]byte, error) {
	if len(b) == 0 {
		return b, nil
	}
	n := int(b[len(b)-1])
	if n == 0 || n > bs || n > len(b) {
		return nil, errors.New("aes: bad padding")
	}
	return b[:len(b)-n], nil
}

// rsaRaw is RSA public encryption with RSA_NO_PADDING (textbook RSA), as
// Node's publicEncrypt({key, padding: RSA_NO_PADDING}) used by LX.
func rsaRaw(data []byte, pemKey string) ([]byte, error) {
	blk, _ := pem.Decode([]byte(strings.TrimSpace(pemKey)))
	if blk == nil {
		return nil, errors.New("rsa: invalid PEM key")
	}
	var pub *rsa.PublicKey
	if k, err := x509.ParsePKIXPublicKey(blk.Bytes); err == nil {
		p, ok := k.(*rsa.PublicKey)
		if !ok {
			return nil, errors.New("rsa: not an RSA key")
		}
		pub = p
	} else if p, err2 := x509.ParsePKCS1PublicKey(blk.Bytes); err2 == nil {
		pub = p
	} else {
		return nil, fmt.Errorf("rsa: %v", err)
	}
	k := (pub.N.BitLen() + 7) / 8
	if len(data) > k {
		return nil, errors.New("rsa: data too long for key")
	}
	m := new(big.Int).SetBytes(data)
	c := new(big.Int).Exp(m, big.NewInt(int64(pub.E)), pub.N)
	out := c.Bytes()
	if len(out) < k {
		out = append(make([]byte, k-len(out)), out...)
	}
	return out, nil
}

func zlibOp(op string, data []byte) ([]byte, error) {
	switch op {
	case "inflate":
		r, err := zlib.NewReader(bytes.NewReader(data))
		if err != nil {
			// some servers send raw deflate or gzip
			if gz, gerr := gzip.NewReader(bytes.NewReader(data)); gerr == nil {
				defer gz.Close()
				return io.ReadAll(io.LimitReader(gz, 32<<20))
			}
			fr := flate.NewReader(bytes.NewReader(data))
			defer fr.Close()
			return io.ReadAll(io.LimitReader(fr, 32<<20))
		}
		defer r.Close()
		return io.ReadAll(io.LimitReader(r, 32<<20))
	case "deflate":
		var buf bytes.Buffer
		w := zlib.NewWriter(&buf)
		w.Write(data)
		w.Close()
		return buf.Bytes(), nil
	}
	return nil, fmt.Errorf("unknown zlib op %q", op)
}

// ---- lx.request ---------------------------------------------------------------------

type requestSpec struct {
	Method   string          `json:"method"`
	Headers  map[string]any  `json:"headers"`
	Timeout  float64         `json:"timeout"`
	Body     *string         `json:"body"`
	BodyHex  string          `json:"bodyHex"`
	BodyObj  json.RawMessage `json:"bodyObj"`
	Form     map[string]any  `json:"form"`
	FormData map[string]any  `json:"formData"`
}

type httpResult struct {
	Status     int
	StatusText string
	Headers    map[string]any
	Body       []byte
}

const maxResponse = 8 << 20

func anyString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case nil:
		return ""
	case float64:
		if x == float64(int64(x)) {
			return fmt.Sprint(int64(x))
		}
		return fmt.Sprint(x)
	default:
		b, _ := json.Marshal(x)
		return string(b)
	}
}

func encodeForm(m map[string]any) string {
	v := url.Values{}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v.Set(k, anyString(m[k]))
	}
	return v.Encode()
}

// doHTTP performs one lx.request call.
func doHTTP(ctx context.Context, c *http.Client, rawURL string, spec requestSpec, maxTimeout time.Duration) (*httpResult, error) {
	if _, err := netguard.CheckURL(rawURL); err != nil {
		return nil, err
	}
	timeout := maxTimeout
	if spec.Timeout > 0 && time.Duration(spec.Timeout)*time.Millisecond < timeout {
		timeout = time.Duration(spec.Timeout) * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	headers := http.Header{}
	for k, v := range spec.Headers {
		headers.Set(k, anyString(v))
	}
	var body io.Reader
	switch {
	case spec.Body != nil:
		body = strings.NewReader(*spec.Body)
	case spec.BodyHex != "":
		b, err := hex.DecodeString(spec.BodyHex)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(b)
	case len(spec.BodyObj) > 0 && string(spec.BodyObj) != "null":
		if strings.Contains(strings.ToLower(headers.Get("Content-Type")), "json") {
			body = bytes.NewReader(spec.BodyObj)
		} else {
			var m map[string]any
			if err := json.Unmarshal(spec.BodyObj, &m); err != nil {
				body = bytes.NewReader(spec.BodyObj)
				if headers.Get("Content-Type") == "" {
					headers.Set("Content-Type", "application/json")
				}
			} else {
				body = strings.NewReader(encodeForm(m))
				if headers.Get("Content-Type") == "" {
					headers.Set("Content-Type", "application/x-www-form-urlencoded")
				}
			}
		}
	case spec.Form != nil:
		body = strings.NewReader(encodeForm(spec.Form))
		headers.Set("Content-Type", "application/x-www-form-urlencoded")
	case spec.FormData != nil:
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		for k, v := range spec.FormData {
			mw.WriteField(k, anyString(v))
		}
		mw.Close()
		body = &buf
		headers.Set("Content-Type", mw.FormDataContentType())
	}
	method := strings.ToUpper(spec.Method)
	if method == "" {
		method = http.MethodGet
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return nil, err
	}
	req.Header = headers
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", netguard.UserAgent)
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxResponse {
		return nil, errors.New("response too large")
	}
	h := map[string]any{}
	for k, vs := range resp.Header {
		lk := strings.ToLower(k)
		if lk == "set-cookie" {
			h[lk] = vs
		} else {
			h[lk] = strings.Join(vs, ", ")
		}
	}
	text := resp.Status
	if i := strings.IndexByte(text, ' '); i >= 0 {
		text = text[i+1:]
	}
	return &httpResult{Status: resp.StatusCode, StatusText: text, Headers: h, Body: b}, nil
}
