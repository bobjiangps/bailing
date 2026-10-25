package lx

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"bailing/internal/netguard"
)

const fakeScript = `/**
 * @name 测试源
 * @version 1.0.0
 * @author bailing
 */
const { EVENT_NAMES, request, on, send, utils } = globalThis.lx
const API = '__API__'
on(EVENT_NAMES.request, ({ source, action, info }) => {
  if (action !== 'musicUrl') return Promise.reject(new Error('unsupported action'))
  return new Promise((resolve, reject) => {
    request(API + '/url?id=' + info.musicInfo.songmid + '&q=' + info.type, {
      method: 'get',
      headers: { 'X-Md5': utils.crypto.md5('abc') },
    }, (err, resp, body) => {
      if (err) return reject(err)
      if (resp.statusCode !== 200) return reject(new Error('status ' + resp.statusCode))
      setTimeout(() => resolve(body.url), 10)
    })
  })
})
const b = utils.buffer.from('落雪', 'utf8')
if (utils.buffer.bufToString(b, 'hex') !== 'e890bde99baa') throw new Error('buffer hex')
if (utils.buffer.from(b.toString('base64'), 'base64').toString() !== '落雪') throw new Error('base64')
const enc = utils.crypto.aesEncrypt(utils.buffer.from('hi'), 'aes-128-ecb', utils.buffer.from('0123456789abcdef'), '')
if (enc.length !== 16) throw new Error('aes')
send(EVENT_NAMES.inited, { sources: { kw: { name: '酷我', type: 'music', actions: ['musicUrl'], qualitys: ['128k', '320k'] } } })
`

func TestVMResolve(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Md5") != "900150983cd24fb0d6963f7d28e17f72" {
			http.Error(w, "bad md5", 400)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{
			"url": "https://cdn.example.com/" + r.URL.Query().Get("id") + "_" + r.URL.Query().Get("q") + ".mp3"})
	}))
	defer srv.Close()

	// CheckURL rejects literal private IPs before dialing; use a host name so
	// the test reaches the local server through the AllowPrivate client.
	api := strings.Replace(srv.URL, "127.0.0.1", "localhost", 1)
	src := strings.Replace(fakeScript, "__API__", api, 1)
	client := netguard.NewClient(netguard.Options{AllowPrivate: true, Timeout: 5 * time.Second})
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	vm, err := Start(src, ParseHeader(src), client, 5*time.Second, log)
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()
	if d, ok := vm.Sources()["kw"]; !ok || !d.Supports("musicUrl") || len(d.Qualitys) != 2 {
		t.Fatalf("sources: %+v", vm.Sources())
	}
	got, err := vm.Request(context.Background(), "kw", "musicUrl",
		map[string]any{"type": "320k", "musicInfo": map[string]any{"songmid": "123"}})
	if err != nil || got != "https://cdn.example.com/123_320k.mp3" {
		t.Fatalf("got %q, %v", got, err)
	}
	if _, err := vm.Request(context.Background(), "kw", "lyric", map[string]any{}); err == nil {
		t.Fatal("expected error for unsupported action")
	}
}

func TestVMBadScript(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	client := netguard.NewClient(netguard.Options{Timeout: time.Second})
	if _, err := Start("throw new Error('boom')", ScriptInfo{}, client, time.Second, log); err == nil ||
		!strings.Contains(err.Error(), "boom") {
		t.Fatalf("want boom, got %v", err)
	}
	if _, err := Start("var a = 1", ScriptInfo{}, client, 300*time.Millisecond, log); err == nil {
		t.Fatal("want inited timeout")
	}
}
