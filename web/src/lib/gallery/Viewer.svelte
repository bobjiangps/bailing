<script lang="ts">
	/*
	 * Photo viewer with zoom / pan / free rotation.
	 *
	 * The whole state is four numbers {x, y, s, a} rendered as one CSS
	 * transform: translate(x, y) rotate(a) scale(s), with the frame centred in
	 * the viewport. Gestures are implemented with plain Pointer Events:
	 *   1 pointer        → pan        (Shift / rotation handle → rotate)
	 *   2 pointers       → pinch zoom + two-finger rotate around their midpoint
	 *   wheel            → zoom around the cursor
	 * Rotation is limited to −180°…+180° (positive = clockwise) and snaps to
	 * 0 / ±90 / 180 within ±4°.
	 */
	import { onMount, untrack } from 'svelte';
	import { api, beacon, photoUrl, type Exif, type Photo } from '$lib/api/client';
	import Icon from '$lib/components/Icon.svelte';
	import { t } from '$lib/i18n';
	import { prefs } from '$lib/stores/prefs.svelte';
	import { clamp, prefersReducedMotion } from '$lib/util/format';
	import { springTransform, tween } from '$lib/util/motion';

	let {
		photos,
		index,
		rectOf,
		onindex,
		onclose
	}: {
		photos: Photo[];
		index: number;
		rectOf: (i: number) => Promise<DOMRect | null>;
		onindex: (i: number) => void;
		onclose: () => void;
	} = $props();

	const SNAP = 4;
	const reduced = prefersReducedMotion();

	// `index` is only the photo to open with; afterwards the viewer navigates
	// on its own, so take a one-time snapshot on purpose.
	let idx = $state(untrack(() => index));
	let x = $state(0);
	let y = $state(0);
	let s = $state(1);
	let a = $state(0);
	let vw = $state(innerWidth);
	let vh = $state(innerHeight);
	let closing = $state(false);
	let ready = $state(false);
	let loaded = $state(false);
	let src = $state('');
	let snapped = $state(false);
	let exif = $state<Exif | null>(null);

	let stage: HTMLDivElement | undefined = $state();
	let frame: HTMLDivElement | undefined = $state();
	let cancelTween: (() => void) | null = null;
	let retries = 0;

	const photo = $derived(photos[idx]);
	const fit = $derived.by(() => {
		const w = photo?.w || 4;
		const h = photo?.h || 3;
		const k = Math.min((vw * 0.9) / w, (vh * (vw < 700 ? 0.72 : 0.8)) / h);
		return { bw: Math.round(w * k), bh: Math.round(h * k) };
	});
	const maxScale = $derived(Math.max(4, ((photo?.w || 2000) / fit.bw) * 2));
	const norm = (v: number) => ((((v + 180) % 360) + 360) % 360) - 180;
	const shownAngle = $derived(Math.round(norm(a)));

	const transform = $derived(`translate(${x}px, ${y}px) rotate(${a}deg) scale(${s})`);

	// ---- loading ----------------------------------------------------------------
	$effect(() => {
		const p = photo;
		if (!p) return;
		loaded = false;
		retries = 0;
		src = photoUrl(p.id, 'm');
		onindex(idx);
		const timer = setTimeout(() => beacon('/api/stats/view', { kind: 'photo', ref: p.id }), 1000);
		return () => clearTimeout(timer);
	});

	// switch to the original when zoomed beyond the 2048px rendition
	$effect(() => {
		const p = photo;
		if (!p || !loaded || src.endsWith('/o')) return;
		const needed = s * fit.bw * (devicePixelRatio || 1);
		if (Math.max(p.w, p.h) > 2048 && needed > 1900) {
			const hi = photoUrl(p.id, 'o');
			const im = new Image();
			im.onload = () => {
				if (photo?.id === p.id) src = hi;
			};
			im.src = hi;
		}
	});

	$effect(() => {
		const p = photo;
		if (!prefs.showExif || !p) {
			exif = null;
			return;
		}
		const gps = prefs.showGps ? '?gps=1' : '';
		api<Exif>(`/api/photos/${p.id}/exif${gps}`)
			.then((r) => {
				if (photo?.id === p.id) exif = r;
			})
			.catch(() => (exif = {}));
	});

	function onImgError() {
		// rendition still being generated (503): retry a few times
		if (retries++ < 6 && photo) {
			const id = photo.id;
			setTimeout(() => {
				if (photo?.id === id) src = photoUrl(id, 'm') + `?r=${retries}`;
			}, 2500);
		}
	}

	// ---- open / close ----------------------------------------------------------------
	function rectTransform(r: DOMRect) {
		const dx = r.left + r.width / 2 - vw / 2;
		const dy = r.top + r.height / 2 - vh / 2;
		return `translate(${dx}px, ${dy}px) rotate(0deg) scale(${r.width / fit.bw})`;
	}

	onMount(() => {
		const wheelHandler = (e: WheelEvent) => onWheel(e);
		stage?.addEventListener('wheel', wheelHandler, { passive: false });
		(async () => {
			const r = await rectOf(idx);
			ready = true;
			if (r && frame && !reduced) {
				await springTransform(frame, rectTransform(r), transform, { stiffness: 150, damping: 20 });
			}
		})();
		return () => stage?.removeEventListener('wheel', wheelHandler);
	});

	async function close() {
		if (closing) return;
		closing = true;
		cancelTween?.();
		const r = await rectOf(idx);
		if (frame && !reduced) {
			if (r) {
				await springTransform(frame, transform, rectTransform(r), { stiffness: 170, damping: 24, fill: 'forwards' });
			} else {
				await frame
					.animate([{ opacity: 1 }, { opacity: 0, transform: `${transform} scale(0.96)` }], {
						duration: 260,
						fill: 'forwards'
					})
					.finished.catch(() => {});
			}
		}
		onclose();
	}

	function go(delta: number) {
		const n = idx + delta;
		if (n < 0 || n >= photos.length) return;
		cancelTween?.();
		x = y = 0;
		s = 1;
		a = 0;
		idx = n;
	}

	// ---- animation helpers --------------------------------------------------------------
	function animateTo(tx: number, ty: number, ts: number, ta: number, ms = 320) {
		cancelTween?.();
		cancelTween = tween([x, y, s, a], [tx, ty, ts, ta], reduced ? 1 : ms, ([nx, ny, ns, na]) => {
			x = nx;
			y = ny;
			s = ns;
			a = na;
		});
		// keep the angle in −180…180 once the tween is done (visually identical)
		setTimeout(() => (a = norm(a)), (reduced ? 1 : ms) + 20);
	}

	function snapAngle(v: number) {
		const n = norm(v);
		const k = Math.round(n / 90) * 90;
		if (Math.abs(n - k) <= SNAP) {
			return { v: v - n + k, hit: true };
		}
		return { v, hit: false };
	}

	function settle() {
		const sn = snapAngle(a);
		let ts = clamp(s, 1, maxScale);
		let tx = x;
		let ty = y;
		if (ts <= 1.001) {
			ts = 1;
			tx = 0;
			ty = 0;
		}
		animateTo(tx, ty, ts, sn.v, 300);
	}

	function rotateBy(deg: number) {
		const target = Math.round((a + deg) / 90) * 90;
		animateTo(x, y, s, target, 380);
	}

	function zoomAt(px: number, py: number, f: number, animate = false) {
		const ns = clamp(s * f, 0.5, maxScale);
		const k = ns / s;
		const cx = px - vw / 2;
		const cy = py - vh / 2;
		const nx = cx - k * (cx - x);
		const ny = cy - k * (cy - y);
		if (animate) animateTo(nx, ny, ns, a, 300);
		else {
			x = nx;
			y = ny;
			s = ns;
		}
	}

	function reset() {
		animateTo(0, 0, 1, Math.round(a / 360) * 360, 380);
	}

	// ---- pointer gestures -------------------------------------------------------------------
	const pts = new Map<number, { x: number; y: number }>();
	let mode: 'none' | 'pan' | 'rotate' | 'pinch' = 'none';
	let start = { px: 0, py: 0, x: 0, y: 0, a: 0, ang0: 0 };
	let pinch = { d: 0, ang: 0, mx: 0, my: 0 };
	let moved = false;
	let lastTap = { t: 0, x: 0, y: 0 };
	let tapTimer: ReturnType<typeof setTimeout> | undefined;
	let wheelTimer: ReturnType<typeof setTimeout> | undefined;

	const deg = (r: number) => (r * 180) / Math.PI;
	function angleFromCenter(px: number, py: number) {
		return deg(Math.atan2(py - (vh / 2 + y), px - (vw / 2 + x)));
	}
	function pinchInfo() {
		const [p1, p2] = [...pts.values()];
		return {
			d: Math.hypot(p2.x - p1.x, p2.y - p1.y) || 1,
			ang: deg(Math.atan2(p2.y - p1.y, p2.x - p1.x)),
			mx: (p1.x + p2.x) / 2 - vw / 2,
			my: (p1.y + p2.y) / 2 - vh / 2
		};
	}

	function onDown(e: PointerEvent) {
		if ((e.target as HTMLElement).closest('.ui')) return;
		stage?.setPointerCapture(e.pointerId);
		pts.set(e.pointerId, { x: e.clientX, y: e.clientY });
		cancelTween?.();
		if (pts.size === 1) {
			moved = false;
			const onHandle = !!(e.target as HTMLElement).closest('.handle');
			mode = onHandle || e.shiftKey ? 'rotate' : 'pan';
			start = { px: e.clientX, py: e.clientY, x, y, a, ang0: angleFromCenter(e.clientX, e.clientY) };
		} else if (pts.size === 2) {
			mode = 'pinch';
			moved = true;
			pinch = pinchInfo();
		}
	}

	function onMove(e: PointerEvent) {
		if (!pts.has(e.pointerId)) return;
		pts.set(e.pointerId, { x: e.clientX, y: e.clientY });
		if (Math.hypot(e.clientX - start.px, e.clientY - start.py) > 5) moved = true;
		if (mode === 'pan') {
			x = start.x + e.clientX - start.px;
			y = start.y + e.clientY - start.py;
		} else if (mode === 'rotate') {
			let target = start.a + norm(angleFromCenter(e.clientX, e.clientY) - start.ang0);
			// limit to −180…+180 around the start of this drag
			target = clamp(target, -180, 180);
			const sn = snapAngle(target);
			snapped = sn.hit;
			a = sn.v;
		} else if (mode === 'pinch' && pts.size >= 2) {
			const cur = pinchInfo();
			const f = cur.d / pinch.d;
			const da = norm(cur.ang - pinch.ang);
			const r = (da * Math.PI) / 180;
			const tx = x - pinch.mx;
			const ty = y - pinch.my;
			const ns = clamp(s * f, 0.4, maxScale * 1.2);
			const k = ns / s;
			x = cur.mx + k * (Math.cos(r) * tx - Math.sin(r) * ty);
			y = cur.my + k * (Math.sin(r) * tx + Math.cos(r) * ty);
			s = ns;
			a = a + da;
			pinch = cur;
		}
	}

	function onUp(e: PointerEvent) {
		if (!pts.has(e.pointerId)) return;
		pts.delete(e.pointerId);
		if (pts.size === 1 && mode === 'pinch') {
			const [p] = [...pts.values()];
			mode = 'pan';
			start = { px: p.x, py: p.y, x, y, a, ang0: 0 };
			return;
		}
		if (pts.size > 0) return;
		const wasMode = mode;
		mode = 'none';
		snapped = false;
		if (!moved && wasMode === 'pan') onTap(e);
		else settle();
	}

	function onTap(e: PointerEvent) {
		const now = performance.now();
		const isDouble = now - lastTap.t < 300 && Math.hypot(e.clientX - lastTap.x, e.clientY - lastTap.y) < 30;
		lastTap = { t: now, x: e.clientX, y: e.clientY };
		clearTimeout(tapTimer);
		if (isDouble) {
			lastTap.t = 0;
			if (s > 1.05) animateTo(0, 0, 1, a, 320);
			else zoomAt(e.clientX, e.clientY, 2.5, true);
			return;
		}
		const onImage = (e.target as HTMLElement).closest('.frame');
		if (!onImage) tapTimer = setTimeout(close, 280); // single tap on the backdrop closes
	}

	function onWheel(e: WheelEvent) {
		e.preventDefault();
		const f = Math.exp(-e.deltaY * (e.ctrlKey ? 0.01 : 0.0018));
		zoomAt(e.clientX, e.clientY, f);
		clearTimeout(wheelTimer);
		wheelTimer = setTimeout(settle, 220);
	}

	function onKey(e: KeyboardEvent) {
		switch (e.key) {
			case 'Escape':
				close();
				break;
			case 'ArrowLeft':
				go(-1);
				break;
			case 'ArrowRight':
				go(1);
				break;
			case 'r':
				rotateBy(90);
				break;
			case 'R':
				rotateBy(-90);
				break;
			case '0':
				reset();
				break;
			case 'i':
			case 'I':
				prefs.set('showExif', !prefs.showExif);
				break;
			case '+':
			case '=':
				zoomAt(vw / 2, vh / 2, 1.4, true);
				break;
			case '-':
				zoomAt(vw / 2, vh / 2, 1 / 1.4, true);
				setTimeout(settle, 320);
				break;
			default:
				return;
		}
		e.stopPropagation();
	}
</script>

<svelte:window
	bind:innerWidth={vw}
	bind:innerHeight={vh}
	onkeydown={onKey}
/>

<div class="viewer" class:closing role="dialog" aria-label={photo?.name}>
	<div class="backdrop"></div>

	<div
		class="stage"
		bind:this={stage}
		onpointerdown={onDown}
		onpointermove={onMove}
		onpointerup={onUp}
		onpointercancel={onUp}
		role="presentation"
	>
		<div
			class="frame"
			bind:this={frame}
			class:ready
			style:width="{fit.bw}px"
			style:height="{fit.bh}px"
			style:margin-left="{-fit.bw / 2}px"
			style:margin-top="{-fit.bh / 2}px"
			style:transform
			style:--s={s}
			style:background={loaded ? 'transparent' : photo?.color || 'var(--accent-soft)'}
		>
			{#key photo?.id}
				<img {src} alt={photo?.name} draggable="false" class:loaded onload={() => (loaded = true)} onerror={onImgError} />
			{/key}
			<span class="handle" title={t('viewer.rotateHandle')}><span class="knob"><Icon name="cw" size={14} /></span></span>
		</div>
	</div>

	<!-- chrome -->
	<button class="ui close icon-btn glass" aria-label={t('viewer.close')} onclick={close}><Icon name="x" size={20} /></button>

	{#if idx > 0}
		<button class="ui side left icon-btn" aria-label={t('viewer.prev')} onclick={() => go(-1)}><Icon name="left" size={26} /></button>
	{/if}
	{#if idx < photos.length - 1}
		<button class="ui side right icon-btn" aria-label={t('viewer.next')} onclick={() => go(1)}><Icon name="right" size={26} /></button>
	{/if}

	<div class="ui toolbar glass">
		<button class="icon-btn" title={t('viewer.rotateLeft')} onclick={() => rotateBy(-90)}><Icon name="ccw" size={18} /></button>
		<button
			class="angle num"
			class:snapped
			class:zero={shownAngle === 0}
			title={t('viewer.reset')}
			ondblclick={reset}
		>
			{shownAngle > 0 ? '↻' : shownAngle < 0 ? '↺' : ''} {Math.abs(shownAngle)}°
		</button>
		<button class="icon-btn" title={t('viewer.rotateRight')} onclick={() => rotateBy(90)}><Icon name="cw" size={18} /></button>
		<span class="sep"></span>
		<button class="icon-btn hide-s" title={t('viewer.zoomOut')} onclick={() => (zoomAt(vw / 2, vh / 2, 1 / 1.4, true), setTimeout(settle, 320))}>
			<Icon name="zoomOut" size={18} />
		</button>
		<span class="zoom num">{Math.round(s * 100)}%</span>
		<button class="icon-btn hide-s" title={t('viewer.zoomIn')} onclick={() => zoomAt(vw / 2, vh / 2, 1.4, true)}><Icon name="zoomIn" size={18} /></button>
		<button class="icon-btn" title={t('viewer.reset')} onclick={reset}><Icon name="reset" size={18} /></button>
		<span class="sep"></span>
		<button
			class="icon-btn"
			class:active={prefs.showExif}
			title={t('viewer.exif')}
			aria-pressed={prefs.showExif}
			onclick={() => prefs.set('showExif', !prefs.showExif)}
		>
			<Icon name="info" size={18} />
		</button>
		<span class="count num">{idx + 1} / {photos.length}</span>
	</div>

	{#if prefs.showExif && exif}
		<aside class="ui exif glass">
			{#if exif.model || exif.lens || exif.fnumber || exif.taken}
				<dl>
					{#if exif.model}<dt>{t('exif.camera')}</dt><dd>{[exif.make, exif.model].filter(Boolean).join(' ')}</dd>{/if}
					{#if exif.lens}<dt>{t('exif.lens')}</dt><dd>{exif.lens}</dd>{/if}
					{#if exif.focal}<dt>{t('exif.focal')}</dt><dd>{exif.focal}</dd>{/if}
					{#if exif.fnumber}<dt>{t('exif.aperture')}</dt><dd>{exif.fnumber}</dd>{/if}
					{#if exif.exposure}<dt>{t('exif.shutter')}</dt><dd>{exif.exposure}</dd>{/if}
					{#if exif.iso}<dt>{t('exif.iso')}</dt><dd>{exif.iso}</dd>{/if}
					{#if exif.taken}<dt>{t('exif.taken')}</dt><dd>{exif.taken}</dd>{/if}
					{#if exif.width}<dt>{t('exif.size')}</dt><dd class="num">{exif.width} × {exif.height}</dd>{/if}
					{#if exif.lat}<dt>{t('exif.location')}</dt><dd class="num">{exif.lat.toFixed(5)}, {exif.lon?.toFixed(5)}</dd>{/if}
				</dl>
			{:else}
				<p class="muted">{t('exif.none')}</p>
			{/if}
		</aside>
	{/if}

	<p class="ui hint hide-s">{t('viewer.hint')}</p>
</div>

<style>
	.viewer {
		position: fixed;
		inset: 0;
		z-index: 60;
		touch-action: none;
		user-select: none;
		-webkit-user-select: none;
	}
	.backdrop {
		position: absolute;
		inset: 0;
		background: color-mix(in oklch, var(--bg) 70%, #000 30%);
		opacity: 0.92;
		backdrop-filter: blur(16px);
		-webkit-backdrop-filter: blur(16px);
		animation: fade 380ms var(--ease-soft);
		transition: opacity 360ms var(--ease-soft);
	}
	:global([data-mode='dark']) .backdrop {
		background: rgb(8 10 14 / 0.9);
	}
	.closing .backdrop {
		opacity: 0;
	}
	.closing .ui {
		opacity: 0;
		pointer-events: none;
	}
	.stage {
		position: absolute;
		inset: 0;
		cursor: grab;
		touch-action: none;
	}
	.stage:active {
		cursor: grabbing;
	}
	.frame {
		position: absolute;
		left: 50%;
		top: 50%;
		transform-origin: 50% 50%;
		will-change: transform;
		box-shadow: 0 30px 80px -30px rgb(0 0 0 / 0.6);
		opacity: 0;
	}
	.frame.ready {
		opacity: 1;
	}
	.frame img {
		width: 100%;
		height: 100%;
		object-fit: contain;
		pointer-events: none;
		opacity: 0;
		transition: opacity 300ms var(--ease-soft);
	}
	.frame img.loaded {
		opacity: 1;
	}
	.handle {
		position: absolute;
		left: 50%;
		top: 0;
		width: 2px;
		height: 30px;
		/* keep the handle the same on-screen size whatever the zoom */
		transform: translate(-50%, -100%) scale(calc(1 / var(--s)));
		transform-origin: 50% 100%;
		background: rgb(255 255 255 / 0.6);
		cursor: crosshair;
		opacity: 0;
		transition: opacity 200ms;
	}
	.knob {
		position: absolute;
		left: 50%;
		top: 0;
		transform: translate(-50%, -100%);
		display: grid;
		place-items: center;
		width: 28px;
		height: 28px;
		border-radius: 50%;
		background: var(--surface);
		color: var(--text);
		box-shadow: 0 4px 12px rgb(0 0 0 / 0.35);
	}
	@media (hover: hover) {
		.stage:hover .handle {
			opacity: 1;
		}
	}
	.ui {
		position: absolute;
		z-index: 2;
		transition: opacity 300ms;
	}
	.close {
		top: 14px;
		right: 16px;
	}
	.side {
		top: 50%;
		transform: translateY(-50%);
		width: 52px;
		height: 52px;
		color: #fff;
		background: rgb(0 0 0 / 0.15);
	}
	.side.left {
		left: 14px;
	}
	.side.right {
		right: 14px;
	}
	.toolbar {
		left: 50%;
		bottom: calc(18px + env(safe-area-inset-bottom));
		transform: translateX(-50%);
		display: flex;
		align-items: center;
		gap: 2px;
		padding: 4px 10px;
		border-radius: 999px;
		box-shadow: 0 14px 40px -18px rgb(0 0 0 / 0.5);
		white-space: nowrap;
	}
	.angle {
		min-width: 64px;
		padding: 2px 8px;
		border-radius: 999px;
		font-size: 13px;
		background: var(--accent-soft);
		transition: transform 160ms var(--ease-soft), background 160ms;
	}
	.angle.zero {
		background: transparent;
		color: var(--text-muted);
	}
	.angle.snapped {
		transform: scale(1.12);
		background: var(--accent);
		color: var(--surface);
	}
	.zoom,
	.count {
		font-size: 12px;
		color: var(--text-muted);
		min-width: 44px;
		text-align: center;
	}
	.sep {
		width: 1px;
		height: 18px;
		background: var(--line);
		margin: 0 4px;
	}
	.exif {
		right: 16px;
		top: 64px;
		width: 240px;
		padding: 14px 16px;
		border-radius: var(--radius-m);
		font-size: 13px;
		animation: fade 300ms var(--ease-soft);
	}
	dl {
		margin: 0;
		display: grid;
		grid-template-columns: auto 1fr;
		gap: 6px 12px;
	}
	dt {
		color: var(--text-muted);
	}
	dd {
		margin: 0;
		word-break: break-word;
	}
	.hint {
		left: 0;
		right: 0;
		top: 18px;
		margin: 0;
		text-align: center;
		font-size: 12px;
		color: rgb(255 255 255 / 0.55);
		pointer-events: none;
	}
	@keyframes fade {
		from {
			opacity: 0;
		}
	}
	@media (max-width: 700px) {
		.hide-s {
			display: none;
		}
		.side {
			display: none;
		}
		.exif {
			left: 12px;
			right: 12px;
			width: auto;
			top: auto;
			bottom: 84px;
		}
	}
</style>

