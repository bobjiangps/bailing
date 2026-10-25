package lx

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/dop251/goja"

	"bailing/internal/netguard"
)

//go:embed prelude.js
var preludeJS string

// SourceDecl is what a script declares for one platform in
// lx.send('inited', { sources: { kw: {...}, ... } }).
type SourceDecl struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Actions  []string `json:"actions"`
	Qualitys []string `json:"qualitys"`
}

// Supports reports whether the declaration lists an action.
func (d SourceDecl) Supports(action string) bool {
	if len(d.Actions) == 0 {
		return action == "musicUrl" // very old scripts omit actions
	}
	for _, a := range d.Actions {
		if a == action {
			return true
		}
	}
	return false
}

type result struct{ val, err string }

type task struct {
	f     func()
	limit time.Duration
}

// syncLimit bounds one synchronous run of script code (not the time spent
// waiting for HTTP, which is asynchronous).
const (
	syncLimit = 5 * time.Second
	bootLimit = 15 * time.Second
)

// VM runs one LX custom-source script in an isolated goja runtime. All
// JavaScript executes on the VM's own goroutine (goja is not thread-safe);
// timers and HTTP responses are posted back to it as tasks.
type VM struct {
	rt      *goja.Runtime
	host    *goja.Object
	tasks   chan task
	quit    chan struct{}
	once    sync.Once
	client  *http.Client
	log     *slog.Logger
	timeout time.Duration

	initedOnce sync.Once
	inited     chan struct{}

	mu        sync.Mutex
	sources   map[string]SourceDecl
	pending   map[int64]chan result
	requests  map[int64]context.CancelFunc
	nextID    int64
	updateMsg string
}

// Start boots a script and waits until it calls lx.send('inited').
func Start(src string, info ScriptInfo, client *http.Client, timeout time.Duration, log *slog.Logger) (*VM, error) {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	v := &VM{tasks: make(chan task, 256), quit: make(chan struct{}), client: client, log: log,
		timeout: timeout, inited: make(chan struct{}), sources: map[string]SourceDecl{},
		pending: map[int64]chan result{}, requests: map[int64]context.CancelFunc{}}
	v.rt = goja.New()
	v.rt.SetMaxCallStackSize(4000)
	go v.loop()

	errc := make(chan error, 1)
	v.postLimit(func() { errc <- v.boot(src, info) }, bootLimit)
	select {
	case err := <-errc:
		if err != nil {
			v.Close()
			return nil, errString(err)
		}
	case <-time.After(bootLimit + time.Second):
		v.Close()
		return nil, errors.New("script start timed out")
	}
	select {
	case <-v.inited:
		return v, nil
	case <-time.After(timeout):
		v.Close()
		return nil, errors.New("script did not call lx.send('inited') in time")
	}
}

func (v *VM) boot(src string, info ScriptInfo) error {
	info.RawScript = src
	ib, _ := json.Marshal(info)
	g := v.rt.NewObject()
	set := func(name string, fn any) { _ = g.Set(name, fn) }

	set("register", func(obj goja.Value) { v.host = obj.ToObject(v.rt) })
	set("ua", func() string { return netguard.UserAgent })
	set("log", func(level, msg string) {
		switch level {
		case "error", "warn":
			v.log.Warn("lx script", "msg", msg)
		default:
			v.log.Debug("lx script", "msg", msg)
		}
	})
	set("timer", func(id int64, ms float64) {
		if ms < 0 || math.IsNaN(ms) || math.IsInf(ms, 0) {
			ms = 0
		}
		time.AfterFunc(time.Duration(ms*float64(time.Millisecond)), func() {
			v.post(func() { v.callHost("fire", id) })
		})
	})
	set("request", func(id int64, rawURL, specJSON string) { v.startRequest(id, rawURL, specJSON) })
	set("abort", func(id int64) {
		v.mu.Lock()
		if c := v.requests[id]; c != nil {
			c()
		}
		v.mu.Unlock()
	})
	set("done", func(id int64, val, errMsg string) {
		v.mu.Lock()
		ch := v.pending[id]
		v.mu.Unlock()
		if ch != nil {
			select {
			case ch <- result{val: val, err: errMsg}:
			default:
			}
		}
	})
	set("send", func(name, data string) { v.onSend(name, data) })

	// binary helpers: everything crosses the bridge as hex strings
	set("utf8enc", func(s string) string { return hex.EncodeToString([]byte(s)) })
	set("utf8dec", func(h string) string { return strings.ToValidUTF8(string(v.unhex(h)), "\uFFFD") })
	set("b64enc", func(h string) string { return base64.StdEncoding.EncodeToString(v.unhex(h)) })
	set("b64dec", func(s string) string { return hex.EncodeToString(v.unb64(s)) })
	set("md5", func(h string) string { return md5Hex(v.unhex(h)) })
	set("aes", func(mode, dataHex, keyHex, ivHex string, decrypt bool) string {
		out, err := aesCrypt(mode, v.unhex(dataHex), v.unhex(keyHex), v.unhex(ivHex), decrypt)
		if err != nil {
			v.throw(err)
		}
		return hex.EncodeToString(out)
	})
	set("rsa", func(dataHex, key string) string {
		out, err := rsaRaw(v.unhex(dataHex), key)
		if err != nil {
			v.throw(err)
		}
		return hex.EncodeToString(out)
	})
	set("random", func(n int) string {
		if n < 0 || n > 1<<16 {
			v.throw(errors.New("randomBytes: bad size"))
		}
		b := make([]byte, n)
		rand.Read(b)
		return hex.EncodeToString(b)
	})
	set("zlib", func(op, h string) string {
		out, err := zlibOp(op, v.unhex(h))
		if err != nil {
			v.throw(err)
		}
		return hex.EncodeToString(out)
	})

	if err := v.rt.Set("__go", g); err != nil {
		return err
	}
	if err := v.rt.Set("__bl_info", string(ib)); err != nil {
		return err
	}
	if _, err := v.rt.RunScript("lx-prelude.js", preludeJS); err != nil {
		return fmt.Errorf("prelude: %w", err)
	}
	if v.host == nil {
		return errors.New("prelude did not register")
	}
	name := info.Name
	if name == "" {
		name = "script"
	}
	_, err := v.rt.RunScript(name+".js", src)
	return err
}

func (v *VM) throw(err error) { panic(v.rt.NewGoError(err)) }

func (v *VM) unhex(h string) []byte {
	b, err := hex.DecodeString(h)
	if err != nil {
		v.throw(errors.New("invalid binary data"))
	}
	return b
}

func (v *VM) unb64(s string) []byte {
	s = strings.Map(func(r rune) rune {
		if r == ' ' || r == '\n' || r == '\r' || r == '\t' {
			return -1
		}
		return r
	}, s)
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding,
		base64.URLEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(s); err == nil {
			return b
		}
	}
	v.throw(errors.New("invalid base64"))
	return nil
}

func (v *VM) onSend(name, data string) {
	switch name {
	case "inited":
		var d struct {
			Sources map[string]SourceDecl `json:"sources"`
		}
		if err := json.Unmarshal([]byte(data), &d); err != nil {
			v.log.Warn("lx inited: bad payload", "err", err)
		}
		src := map[string]SourceDecl{}
		for k, s := range d.Sources {
			if s.Type == "" || s.Type == "music" {
				src[k] = s
			}
		}
		v.mu.Lock()
		v.sources = src
		v.mu.Unlock()
		v.initedOnce.Do(func() { close(v.inited) })
	case "updateAlert":
		var d struct {
			Log       string `json:"log"`
			UpdateURL string `json:"updateUrl"`
		}
		json.Unmarshal([]byte(data), &d)
		v.mu.Lock()
		v.updateMsg = clip(strings.TrimSpace(d.Log+" "+d.UpdateURL), 500)
		v.mu.Unlock()
	}
}

func (v *VM) startRequest(id int64, rawURL, specJSON string) {
	var spec requestSpec
	if err := json.Unmarshal([]byte(specJSON), &spec); err != nil {
		v.post(func() { v.callHost("response", id, "bad request options", 0, "", "{}", "", "") })
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	v.mu.Lock()
	v.requests[id] = cancel
	v.mu.Unlock()
	go func() {
		res, err := doHTTP(ctx, v.client, rawURL, spec, v.timeout)
		v.mu.Lock()
		delete(v.requests, id)
		v.mu.Unlock()
		cancel()
		v.post(func() {
			if err != nil {
				v.callHost("response", id, err.Error(), 0, "", "{}", "", "")
				return
			}
			hb, _ := json.Marshal(res.Headers)
			v.callHost("response", id, "", res.Status, res.StatusText, string(hb),
				hex.EncodeToString(res.Body), strings.ToValidUTF8(string(res.Body), "\uFFFD"))
		})
	}()
}

// ---- event loop ------------------------------------------------------------------

func (v *VM) loop() {
	for {
		select {
		case t := <-v.tasks:
			v.run(t)
		case <-v.quit:
			return
		}
	}
}

func (v *VM) run(t task) {
	timer := time.AfterFunc(t.limit, func() { v.rt.Interrupt("script ran too long") })
	defer func() {
		timer.Stop()
		v.rt.ClearInterrupt()
		if r := recover(); r != nil {
			v.log.Warn("lx vm panic", "err", r)
		}
	}()
	t.f()
}

func (v *VM) post(f func()) bool { return v.postLimit(f, syncLimit) }

func (v *VM) postLimit(f func(), limit time.Duration) bool {
	select {
	case <-v.quit:
		return false
	default:
	}
	select {
	case v.tasks <- task{f: f, limit: limit}:
		return true
	case <-v.quit:
		return false
	}
}

// callHost calls a function of the prelude's host object (loop goroutine only).
func (v *VM) callHost(name string, args ...any) {
	if v.host == nil {
		return
	}
	fn, ok := goja.AssertFunction(v.host.Get(name))
	if !ok {
		return
	}
	vals := make([]goja.Value, len(args))
	for i, a := range args {
		vals[i] = v.rt.ToValue(a)
	}
	if _, err := fn(goja.Undefined(), vals...); err != nil {
		v.log.Warn("lx script error", "in", name, "err", errString(err))
	}
}

// ---- public API ------------------------------------------------------------------

// Request sends a `request` event ({source, action, info}) to the script
// and waits for the handler's result (a string, e.g. the music URL).
func (v *VM) Request(ctx context.Context, source, action string, info any) (string, error) {
	ib, err := json.Marshal(info)
	if err != nil {
		return "", err
	}
	v.mu.Lock()
	v.nextID++
	id := v.nextID
	ch := make(chan result, 1)
	v.pending[id] = ch
	v.mu.Unlock()
	defer func() {
		v.mu.Lock()
		delete(v.pending, id)
		v.mu.Unlock()
	}()
	if !v.post(func() { v.callHost("dispatch", id, source, action, string(ib)) }) {
		return "", errors.New("script stopped")
	}
	ctx, cancel := context.WithTimeout(ctx, v.timeout)
	defer cancel()
	select {
	case r := <-ch:
		if r.err != "" {
			return "", errors.New(r.err)
		}
		return r.val, nil
	case <-ctx.Done():
		return "", errors.New("script timed out")
	}
}

// Sources returns the platforms the script declared.
func (v *VM) Sources() map[string]SourceDecl {
	v.mu.Lock()
	defer v.mu.Unlock()
	out := make(map[string]SourceDecl, len(v.sources))
	for k, s := range v.sources {
		out[k] = s
	}
	return out
}

// UpdateMessage returns the script's latest updateAlert text, if any.
func (v *VM) UpdateMessage() string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.updateMsg
}

// Close stops the VM and cancels outstanding requests.
func (v *VM) Close() {
	v.once.Do(func() {
		close(v.quit)
		v.rt.Interrupt("closed")
		v.mu.Lock()
		for _, c := range v.requests {
			c()
		}
		v.mu.Unlock()
	})
}

func errString(err error) error {
	var ex *goja.Exception
	if errors.As(err, &ex) {
		msg := ex.Error()
		if i := strings.Index(msg, "\n"); i > 0 {
			msg = msg[:i]
		}
		return errors.New(clip(msg, 300))
	}
	return err
}
