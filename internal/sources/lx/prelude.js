// Host prelude for LX Music custom-source scripts. Runs before the user
// script inside an isolated goja VM. It builds the global `lx` object that
// LX Music desktop provides to custom sources (lx.on / lx.send /
// lx.request / lx.utils / lx.EVENT_NAMES ...) plus the small browser-like
// globals scripts tend to use (setTimeout, console, fetch, atob, btoa,
// TextEncoder/TextDecoder).
//
// All bridging to Go goes through `go` (captured here, then removed from
// the global scope) and plain strings (hex for binary data), which keeps the
// Go side simple and robust.
(function (g) {
	'use strict';
	var go = g.__go;
	var info = JSON.parse(g.__bl_info || '{}');
	// hide the bridge from the user script
	try {
		delete g.__go;
		delete g.__bl_info;
	} catch (e) {
		g.__go = undefined;
		g.__bl_info = undefined;
	}

	// ---- bytes ------------------------------------------------------------------
	var HEX = '0123456789abcdef';
	function toHex(u8) {
		var s = '';
		for (var i = 0; i < u8.length; i++) s += HEX[u8[i] >> 4] + HEX[u8[i] & 15];
		return s;
	}
	function fromHex(h) {
		var n = h.length >> 1;
		var u = new Uint8Array(n);
		for (var i = 0; i < n; i++) u[i] = parseInt(h.substr(i * 2, 2), 16);
		return u;
	}

	var BufProto = Object.create(Uint8Array.prototype);
	function mkBuf(u8) {
		Object.setPrototypeOf(u8, BufProto);
		return u8;
	}
	function isBuf(x) {
		return x instanceof Uint8Array;
	}
	function normEnc(e) {
		e = String(e || 'utf8').toLowerCase().replace('-', '');
		if (e === 'latin1' || e === 'ascii') return 'binary';
		return e;
	}
	function bytesOf(data, enc) {
		if (data == null) return new Uint8Array(0);
		if (isBuf(data)) return new Uint8Array(data);
		if (data instanceof ArrayBuffer) return new Uint8Array(data.slice(0));
		if (ArrayBuffer.isView(data)) return new Uint8Array(data.buffer.slice(data.byteOffset, data.byteOffset + data.byteLength));
		if (Array.isArray(data)) return new Uint8Array(data);
		if (typeof data === 'object' && data.type === 'Buffer' && Array.isArray(data.data)) return new Uint8Array(data.data);
		var s = String(data);
		switch (normEnc(enc)) {
			case 'hex':
				return fromHex(s);
			case 'base64':
				return fromHex(go.b64dec(s));
			case 'binary': {
				var u = new Uint8Array(s.length);
				for (var i = 0; i < s.length; i++) u[i] = s.charCodeAt(i) & 255;
				return u;
			}
			default:
				return fromHex(go.utf8enc(s));
		}
	}
	function strOf(u8, enc) {
		switch (normEnc(enc)) {
			case 'hex':
				return toHex(u8);
			case 'base64':
				return go.b64enc(toHex(u8));
			case 'binary': {
				var s = '';
				for (var i = 0; i < u8.length; i++) s += String.fromCharCode(u8[i]);
				return s;
			}
			default:
				return go.utf8dec(toHex(u8));
		}
	}
	BufProto.toString = function (enc, start, end) {
		var u = this;
		if (start !== undefined || end !== undefined) u = this.subarray(start || 0, end === undefined ? this.length : end);
		return strOf(u, enc);
	};
	BufProto.toJSON = function () {
		return { type: 'Buffer', data: Array.prototype.slice.call(this) };
	};
	BufProto.equals = function (o) {
		if (!isBuf(o) || o.length !== this.length) return false;
		for (var i = 0; i < o.length; i++) if (o[i] !== this[i]) return false;
		return true;
	};
	BufProto.slice = function (a, b) {
		return mkBuf(Uint8Array.prototype.slice.call(this, a, b));
	};
	BufProto.write = function (str, offset, enc) {
		var b = bytesOf(str, enc);
		this.set(b, offset || 0);
		return b.length;
	};

	var Buffer = {
		from: function (data, enc) {
			return mkBuf(bytesOf(data, enc));
		},
		alloc: function (n, fill) {
			var u = new Uint8Array(n);
			if (fill) u.fill(typeof fill === 'number' ? fill : bytesOf(fill)[0] || 0);
			return mkBuf(u);
		},
		allocUnsafe: function (n) {
			return mkBuf(new Uint8Array(n));
		},
		isBuffer: function (x) {
			return isBuf(x) && Object.getPrototypeOf(x) === BufProto;
		},
		byteLength: function (s, enc) {
			return bytesOf(s, enc).length;
		},
		concat: function (list, total) {
			var n = 0;
			for (var i = 0; i < list.length; i++) n += list[i].length;
			var out = new Uint8Array(total === undefined ? n : total);
			var off = 0;
			for (var j = 0; j < list.length && off < out.length; j++) {
				out.set(list[j].subarray(0, out.length - off), off);
				off += list[j].length;
			}
			return mkBuf(out);
		}
	};
	BufProto.constructor = Uint8Array;

	// ---- timers -----------------------------------------------------------------
	var timers = {};
	var tid = 0;
	function addTimer(fn, ms, args, repeat) {
		var id = ++tid;
		ms = Math.max(0, +ms || 0);
		timers[id] = { fn: fn, args: args, repeat: repeat ? Math.max(ms, 10) : 0 };
		go.timer(id, repeat ? Math.max(ms, 10) : ms);
		return id;
	}
	g.setTimeout = function (fn, ms) {
		return addTimer(fn, ms, Array.prototype.slice.call(arguments, 2), false);
	};
	g.setInterval = function (fn, ms) {
		return addTimer(fn, ms, Array.prototype.slice.call(arguments, 2), true);
	};
	g.clearTimeout = g.clearInterval = function (id) {
		delete timers[id];
	};
	g.setImmediate = function (fn) {
		return addTimer(fn, 0, Array.prototype.slice.call(arguments, 1), false);
	};

	// ---- console ----------------------------------------------------------------
	function fmt(args) {
		var out = [];
		for (var i = 0; i < args.length; i++) {
			var a = args[i];
			if (typeof a === 'string') out.push(a);
			else if (a instanceof Error) out.push(a.stack || a.message);
			else {
				try {
					out.push(JSON.stringify(a));
				} catch (e) {
					out.push(String(a));
				}
			}
		}
		return out.join(' ').slice(0, 2000);
	}
	g.console = {
		log: function () { go.log('info', fmt(arguments)); },
		info: function () { go.log('info', fmt(arguments)); },
		debug: function () { go.log('debug', fmt(arguments)); },
		warn: function () { go.log('warn', fmt(arguments)); },
		error: function () { go.log('error', fmt(arguments)); },
		group: function () {},
		groupEnd: function () {},
		time: function () {},
		timeEnd: function () {}
	};

	// ---- http -------------------------------------------------------------------
	var reqs = {};
	var rid = 0;
	function doRequest(url, opts, cb) {
		opts = opts || {};
		if (typeof cb !== 'function') cb = function () {};
		var id = ++rid;
		reqs[id] = cb;
		var spec = {
			method: String(opts.method || 'get').toUpperCase(),
			headers: opts.headers || {},
			timeout: +opts.timeout || 0
		};
		if (opts.body != null) {
			if (typeof opts.body === 'string') spec.body = opts.body;
			else if (isBuf(opts.body) || opts.body instanceof ArrayBuffer) spec.bodyHex = toHex(bytesOf(opts.body));
			else spec.bodyObj = opts.body;
		} else if (opts.form != null) {
			spec.form = opts.form;
		} else if (opts.formData != null) {
			spec.formData = opts.formData;
		}
		go.request(id, String(url), JSON.stringify(spec));
		return function () {
			if (reqs[id]) {
				delete reqs[id];
				go.abort(id);
			}
		};
	}

	var host = {};
	host.response = function (id, err, status, statusMessage, headersJSON, bodyHex, bodyText) {
		var cb = reqs[id];
		if (!cb) return;
		delete reqs[id];
		if (err) {
			cb.call(lx, new Error(err), null, null);
			return;
		}
		var raw = mkBuf(fromHex(bodyHex));
		var body = bodyText;
		try {
			body = JSON.parse(bodyText);
		} catch (e) {
			/* not JSON */
		}
		var resp = {
			statusCode: status,
			statusMessage: statusMessage,
			headers: JSON.parse(headersJSON),
			bytes: raw.length,
			raw: raw,
			body: body
		};
		cb.call(lx, null, resp, body);
	};
	host.fire = function (id) {
		var t = timers[id];
		if (!t) return;
		if (t.repeat) go.timer(id, t.repeat);
		else delete timers[id];
		if (typeof t.fn === 'function') t.fn.apply(null, t.args);
	};

	function headerObj(h) {
		var out = {};
		if (!h) return out;
		if (typeof h.forEach === 'function' && !Array.isArray(h)) {
			h.forEach(function (v, k) { out[k] = v; });
		} else if (Array.isArray(h)) {
			h.forEach(function (p) { out[p[0]] = p[1]; });
		} else {
			for (var k in h) out[k] = h[k];
		}
		return out;
	}
	g.fetch = function (input, init) {
		init = init || {};
		var url = typeof input === 'string' ? input : input && input.url;
		return new Promise(function (resolve, reject) {
			doRequest(String(url), { method: init.method || 'GET', headers: headerObj(init.headers), body: init.body }, function (err, resp) {
				if (err) return reject(new TypeError('fetch failed: ' + err.message));
				var raw = resp.raw;
				resolve({
					ok: resp.statusCode >= 200 && resp.statusCode < 300,
					status: resp.statusCode,
					statusText: resp.statusMessage,
					url: String(url),
					headers: {
						get: function (k) {
							var v = resp.headers[String(k).toLowerCase()];
							return v == null ? null : String(v);
						},
						has: function (k) {
							return resp.headers[String(k).toLowerCase()] != null;
						}
					},
					text: function () { return Promise.resolve(raw.toString('utf8')); },
					json: function () { return Promise.resolve(JSON.parse(raw.toString('utf8'))); },
					arrayBuffer: function () { return Promise.resolve(raw.buffer.slice(raw.byteOffset, raw.byteOffset + raw.length)); }
				});
			});
		});
	};

	// ---- small browser globals ------------------------------------------------------
	g.atob = function (s) { return strOf(bytesOf(String(s), 'base64'), 'binary'); };
	g.btoa = function (s) { return strOf(bytesOf(String(s), 'binary'), 'base64'); };
	g.TextEncoder = function () {};
	g.TextEncoder.prototype.encode = function (s) { return bytesOf(String(s === undefined ? '' : s), 'utf8'); };
	g.TextDecoder = function () {};
	g.TextDecoder.prototype.decode = function (u) { return u ? strOf(bytesOf(u), 'utf8') : ''; };
	g.window = g.self = g;
	g.Buffer = Buffer;
	g.navigator = { userAgent: go.ua() };

	// ---- lx -----------------------------------------------------------------------
	var EVENT_NAMES = { request: 'request', inited: 'inited', updateAlert: 'updateAlert' };
	var handlers = {};

	host.dispatch = function (id, source, action, infoJSON) {
		var h = handlers.request;
		if (typeof h !== 'function') {
			go.done(id, '', 'script did not register a request handler');
			return;
		}
		var reqInfo = null;
		try {
			reqInfo = JSON.parse(infoJSON);
		} catch (e) {
			/* keep null */
		}
		Promise.resolve()
			.then(function () { return h.call(lx, { source: source, action: action, info: reqInfo }); })
			.then(
				function (r) {
					go.done(id, typeof r === 'string' ? r : JSON.stringify(r === undefined ? null : r), '');
				},
				function (e) {
					go.done(id, '', String((e && e.message) || e || 'failed'));
				}
			);
	};

	function cryptoInput(x) {
		return toHex(bytesOf(x));
	}

	var lx = {
		EVENT_NAMES: EVENT_NAMES,
		version: '2.0.0',
		env: 'desktop',
		currentScriptInfo: info,
		request: doRequest,
		send: function (name, data) {
			var json = '';
			try {
				json = JSON.stringify(data === undefined ? null : data);
			} catch (e) {
				json = 'null';
			}
			go.send(String(name), json);
			return Promise.resolve();
		},
		on: function (name, handler) {
			if (name === EVENT_NAMES.request) handlers.request = handler;
			return Promise.resolve();
		},
		utils: {
			buffer: {
				from: Buffer.from,
				bufToString: function (buf, format) { return strOf(bytesOf(buf), format); }
			},
			crypto: {
				aesEncrypt: function (buffer, mode, key, iv) {
					return mkBuf(fromHex(go.aes(String(mode), cryptoInput(buffer), cryptoInput(key), iv == null ? '' : cryptoInput(iv), false)));
				},
				aesDecrypt: function (buffer, mode, key, iv) {
					return mkBuf(fromHex(go.aes(String(mode), cryptoInput(buffer), cryptoInput(key), iv == null ? '' : cryptoInput(iv), true)));
				},
				rsaEncrypt: function (buffer, key) {
					return mkBuf(fromHex(go.rsa(cryptoInput(buffer), String(key))));
				},
				randomBytes: function (size) {
					return mkBuf(fromHex(go.random(size | 0)));
				},
				md5: function (str) {
					return go.md5(cryptoInput(str));
				}
			},
			zlib: {
				inflate: function (buf) {
					return new Promise(function (resolve) { resolve(mkBuf(fromHex(go.zlib('inflate', cryptoInput(buf))))); });
				},
				deflate: function (buf) {
					return new Promise(function (resolve) { resolve(mkBuf(fromHex(go.zlib('deflate', cryptoInput(buf))))); });
				}
			}
		}
	};

	g.lx = lx;
	go.register(host);
})(this);



