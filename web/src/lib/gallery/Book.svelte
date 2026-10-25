<script lang="ts">
	/*
	 * An opened photo album.
	 *
	 * 1. "Push in": a closed copy of the book flies from its place on the shelf
	 *    to the centre (FLIP + spring, Web Animations API).
	 * 2. StPageFlip (page-flip, MIT) takes over at exactly the same position and
	 *    the cover is flipped open. Desktop / landscape = double pages,
	 *    portrait phones = single page.
	 * 3. Pages are created imperatively (PageFlip moves and removes DOM nodes,
	 *    so Svelte must not own them). Images are only loaded near the current
	 *    page, so albums with hundreds of photos stay light.
	 * 4. Closing plays everything backwards.
	 */
	import { onMount, onDestroy } from 'svelte';
	import { api, beacon, photoUrl, type Album, type Photo } from '$lib/api/client';
	import Icon from '$lib/components/Icon.svelte';
	import { t } from '$lib/i18n';
	import { prefersReducedMotion, clamp } from '$lib/util/format';
	import { springTransform } from '$lib/util/motion';
	import Viewer from './Viewer.svelte';

	let { album, origin, onclose }: { album: Album; origin: HTMLElement; onclose: () => void } = $props();

	type Layout = { pageW: number; pageH: number; landscape: boolean; stageW: number; top: number; left: number };

	let photos = $state<Photo[]>([]);
	let phase = $state<'opening' | 'open' | 'closing'>('opening');
	let page = $state(0);
	let viewerIndex = $state<number | null>(null);
	let loadError = $state(false);
	let L = $state<Layout>(computeLayout());

	let closedEl: HTMLDivElement | undefined = $state();
	let host: HTMLDivElement | undefined = $state();
	let block: HTMLDivElement | null = null;
	let flip: import('page-flip').PageFlip | null = null;
	let resizeTimer: ReturnType<typeof setTimeout> | undefined;
	const reduced = prefersReducedMotion();

	const PHOTO_OFFSET = 2; // page 0 = cover, page 1 = title page, photos start at page 2

	/*
	 * Page plan. Photos keep their own aspect ratio. A portrait (or square)
	 * photo gets a page of its own; two landscape photos in a row share one
	 * page, stacked. This fills the 3:4 paper far better than one small
	 * landscape photo floating in the middle of the page.
	 */
	const isWide = (p: Photo) => (p.w || 4) > (p.h || 3) * 1.08;

	function planSheets(list: Photo[]): number[][] {
		const out: number[][] = [];
		for (let i = 0; i < list.length; i++) {
			if (isWide(list[i]) && i + 1 < list.length && isWide(list[i + 1])) {
				out.push([i, i + 1]);
				i++;
			} else out.push([i]);
		}
		return out;
	}

	const sheets = $derived(planSheets(photos));
	/** book page of every photo */
	const photoPage = $derived.by(() => {
		const m: number[] = [];
		sheets.forEach((sh, k) => sh.forEach((i) => (m[i] = k + PHOTO_OFFSET)));
		return m;
	});
	/** number (1-based) of the first photo on the current page */
	const photoNo = $derived.by(() => {
		const k = page - PHOTO_OFFSET;
		if (k < 0) return 0;
		if (k >= sheets.length) return photos.length;
		return clamp(sheets[k][0] + 1, 0, photos.length);
	});

	/** the last photo page is visible (or the back cover): offer a way back */
	const atEnd = $derived(page + (L.landscape ? 1 : 0) >= PHOTO_OFFSET + sheets.length - 1);

	function toStart() {
		if (!flip || page <= 1) return;
		flip.flip(1); // title page (+ first photo on double pages)
	}

	function computeLayout(): Layout {
		const vw = innerWidth;
		const vh = innerHeight;
		const chrome = vh < 520 ? 64 : 190; // close button on top; page no., end buttons and key hints below
		const availH = vh - chrome;
		const landscape = vw >= 700 || vw > vh;
		const availW = vw * 0.94 - (landscape && vw > 900 ? 140 : 0);
		const perPageW = landscape ? availW / 2 : availW;
		const pageH = Math.floor(Math.min(availH, perPageW * (4 / 3), 900));
		const pageW = Math.floor(pageH * 0.75);
		const stageW = landscape ? pageW * 2 : pageW;
		return {
			pageW,
			pageH,
			landscape,
			stageW,
			top: Math.round((vh - pageH) / 2 + (vh < 520 ? 4 : 0)),
			left: Math.round((vw - stageW) / 2)
		};
	}

	/** Where the closed book sits: the right half of the spread (cover side). */
	function closedRect(l: Layout) {
		return { left: l.landscape ? l.left + l.pageW : l.left, top: l.top, width: l.pageW, height: l.pageH };
	}

	function originRect(): DOMRect {
		return (origin.querySelector('.cover') ?? origin).getBoundingClientRect();
	}

	function flipFrom(r: DOMRect, target: { left: number; top: number; width: number; height: number }) {
		const sx = r.width / target.width;
		const sy = r.height / target.height;
		return `translate(${r.left - target.left}px, ${r.top - target.top}px) scale(${sx}, ${sy})`;
	}

	const frames = (n = 2) =>
		new Promise<void>((res) => {
			const step = () => (--n <= 0 ? res() : requestAnimationFrame(step));
			requestAnimationFrame(step);
		});
	const wait = (ms: number) => new Promise((r) => setTimeout(r, ms));

	// ---- pages -----------------------------------------------------------------

	function el<K extends keyof HTMLElementTagNameMap>(tag: K, cls: string, text?: string) {
		const e = document.createElement(tag);
		e.className = cls;
		if (text) e.textContent = text;
		return e;
	}

	/** Largest size that keeps the photo's ratio inside its share of the page. */
	function fitPhoto(p: Photo, pair: boolean) {
		const w = p.w || 4;
		const h = p.h || 3;
		const maxW = L.pageW * (pair ? 0.86 : 0.88);
		// leave room for caption(s), the folio and equal margins top / bottom
		const maxH = L.pageH * (pair ? 0.33 : 0.7);
		const s = Math.min(maxW / w, maxH / h);
		return { w: Math.round(w * s), h: Math.round(h * s) };
	}

	function figure(p: Photo, i: number, pair: boolean) {
		const fig = el('div', 'fig');
		const btn = el('button', 'ph');
		btn.dataset.i = String(i);
		btn.style.setProperty('--r', `${(((i * 37) % 7) - 3) * (pair ? 0.3 : 0.45)}deg`);
		btn.setAttribute('aria-label', p.name);
		const img = el('img', '');
		const sz = fitPhoto(p, pair);
		img.width = sz.w;
		img.height = sz.h;
		img.alt = '';
		img.dataset.src = photoUrl(p.id, 's');
		img.style.background = p.color || 'var(--accent-soft)';
		img.decoding = 'async';
		btn.append(img);
		const cap = p.taken ? new Date(p.taken * 1000).toLocaleDateString() : p.name.replace(/\.[^.]+$/, '');
		fig.append(btn, el('div', 'cap', cap));
		return fig;
	}

	function buildPages(): HTMLElement[] {
		const pages: HTMLElement[] = [];

		const cover = el('div', 'pg pg-hard');
		cover.dataset.density = 'hard';
		if (closedEl) {
			cover.style.setProperty('--cloth', closedEl.style.getPropertyValue('--cloth'));
			cover.appendChild(closedEl.firstElementChild!.cloneNode(true));
		}
		pages.push(cover);

		const intro = el('div', 'pg pg-paper pg-intro');
		const introInner = el('div', 'intro-inner');
		introInner.append(el('h2', 'art', album.title));
		if (album.date) introInner.append(el('p', 'date', album.date));
		if (album.description) introInner.append(el('p', 'desc art', album.description));
		introInner.append(el('p', 'count', t('albums.photos', { n: photos.length })));
		// PageFlip forces `display:block` on every page, so the centring flex
		// box must live on an inner wrapper, not on the page itself.
		const introIn = el('div', 'pg-in');
		introIn.append(introInner);
		intro.append(introIn);
		pages.push(intro);

		sheets.forEach((sh, k) => {
			const pair = sh.length > 1;
			const pg = el('div', pair ? 'pg pg-paper pg-pair' : 'pg pg-paper');
			const inner = el('div', 'pg-in');
			sh.forEach((i) => inner.append(figure(photos[i], i, pair)));
			pg.append(inner, el('div', 'folio num', String(k + 1)));
			pages.push(pg);
		});

		if ((pages.length - 1) % 2 === 0) pages.push(el('div', 'pg pg-paper pg-blank')); // keep spreads even
		const back = el('div', 'pg pg-hard pg-back');
		back.dataset.density = 'hard';
		pages.push(back);
		return pages;
	}

	function loadNear(p: number) {
		if (!block) return;
		const from = Math.max(0, p - 4);
		const to = p + 8;
		block.querySelectorAll<HTMLImageElement>('button.ph img').forEach((img) => {
			const i = Number((img.parentElement as HTMLElement).dataset.i);
			const pg = photoPage[i] ?? 0;
			if (pg >= from && pg <= to && !img.src && img.dataset.src) img.src = img.dataset.src;
		});
	}

	async function initFlip(startPage: number) {
		if (!host) return;
		const { PageFlip } = await import('page-flip');
		destroyFlip();
		block = document.createElement('div');
		block.className = 'flip-block';
		host.appendChild(block);
		const pages = buildPages();
		pages.forEach((p) => block!.appendChild(p));
		const opts: Partial<import('page-flip').FlipSetting> = {
			width: L.pageW,
			height: L.pageH,
			size: 'fixed',
			showCover: true,
			usePortrait: true,
			startPage,
			drawShadow: true,
			maxShadowOpacity: 0.35,
			flippingTime: reduced ? 300 : 900,
			mobileScrollSupport: false,
			clickEventForward: true, // let clicks on <button class="ph"> through
			disableFlipByClick: true,
			useMouseEvents: true,
			swipeDistance: 30,
			showPageCorners: true,
			autoSize: false
		};
		flip = new PageFlip(block, opts);
		flip.loadFromHTML(pages);
		flip.on('flip', (e) => {
			page = e.data as number;
			loadNear(page);
		});
		page = startPage;
		loadNear(startPage);
	}

	function destroyFlip() {
		if (flip) {
			try {
				flip.destroy();
			} catch {
				/* already gone */
			}
		}
		flip = null;
		block?.remove();
		block = null;
	}

	// ---- open / close ------------------------------------------------------------------

	onMount(async () => {
		document.documentElement.style.overflow = 'hidden';
		beacon('/api/stats/view', { kind: 'album', ref: album.id });
		const dataP = api<{ photos: Photo[] }>(`/api/albums/${album.id}`)
			.then((r) => (photos = r.photos))
			.catch(() => (loadError = true));

		const target = closedRect(L);
		if (closedEl && !reduced) {
			await springTransform(closedEl, flipFrom(originRect(), target), 'none', { stiffness: 110, damping: 19 });
		}
		await dataP;
		if (loadError) return;
		await initFlip(0);
		await frames(2);
		phase = 'open';
		await wait(reduced ? 0 : 200);
		flip?.flipNext(); // the cover opens
	});

	onDestroy(() => {
		destroyFlip();
		clearTimeout(resizeTimer);
		document.documentElement.style.overflow = '';
	});

	async function close() {
		if (phase === 'closing') return;
		if (viewerIndex !== null) return;
		if (flip && page > 0 && !reduced) {
			flip.flip(0);
			await wait(950);
		}
		phase = 'closing';
		await frames(1);
		destroyFlip();
		if (closedEl && !reduced) {
			const anim = springTransform(closedEl, 'none', flipFrom(originRect(), closedRect(L)), {
				stiffness: 140,
				damping: 22,
				fill: 'forwards'
			});
			await anim;
		}
		onclose();
	}

	function onResize() {
		clearTimeout(resizeTimer);
		resizeTimer = setTimeout(async () => {
			const next = computeLayout();
			if (next.pageW === L.pageW && next.landscape === L.landscape) return;
			L = next;
			if (phase === 'open') {
				await frames(1);
				initFlip(page);
			}
		}, 250);
	}

	function onKey(e: KeyboardEvent) {
		if (viewerIndex !== null || phase !== 'open') return;
		if (e.key === 'ArrowRight') flip?.flipNext();
		else if (e.key === 'ArrowLeft') flip?.flipPrev();
		else if (e.key === 'Home') toStart();
		else if (e.key === 'Escape') close();
	}

	// ---- photo viewer link -----------------------------------------------------------

	function onHostClick(e: MouseEvent) {
		const btn = (e.target as HTMLElement).closest<HTMLButtonElement>('button.ph');
		if (!btn || phase !== 'open') return;
		const i = Number(btn.dataset.i);
		if (!photos[i]) return;
		setHidden(i);
		viewerIndex = i;
	}

	function imgOf(i: number) {
		return block?.querySelector<HTMLImageElement>(`button.ph[data-i="${i}"] img`) ?? null;
	}

	function setHidden(i: number | null) {
		block?.querySelectorAll<HTMLImageElement>('button.ph img').forEach((img) => {
			img.style.visibility = Number((img.parentElement as HTMLElement).dataset.i) === i ? 'hidden' : '';
		});
	}

	/** Rect of photo i inside the book; turns to its page first if needed. */
	async function rectOf(i: number): Promise<DOMRect | null> {
		let img = imgOf(i);
		let r = img?.getBoundingClientRect();
		if (!r || r.width === 0) {
			flip?.turnToPage(photoPage[i] ?? i + PHOTO_OFFSET);
			page = photoPage[i] ?? i + PHOTO_OFFSET;
			loadNear(page);
			await frames(2);
			img = imgOf(i);
			r = img?.getBoundingClientRect();
		}
		return r && r.width > 0 ? r : null;
	}
</script>

<svelte:window onresize={onResize} onkeydown={onKey} />

<div class="book-overlay" class:closing={phase === 'closing'} role="dialog" aria-label={album.title}>
	<button class="dim" aria-label={t('albums.close')} onclick={close}></button>

	<!-- closed book used for the push-in / push-out animation -->
	<div
		class="closed"
		bind:this={closedEl}
		class:gone={phase === 'open'}
		style:left="{closedRect(L).left}px"
		style:top="{closedRect(L).top}px"
		style:width="{L.pageW}px"
		style:height="{L.pageH}px"
		style:--cloth={album.color ? `color-mix(in oklch, ${album.color} 55%, var(--book-cloth))` : 'var(--book-cloth)'}
	>
		<div class="face">
			<span class="face-spine"></span>
			<span class="face-photo">
				{#if album.cover}<img src={photoUrl(album.cover, 's')} alt="" />{/if}
			</span>
			<span class="face-title art">{album.title}</span>
			{#if album.date}<span class="face-date">{album.date}</span>{/if}
		</div>
	</div>

	<!-- PageFlip mounts into this host. The click handler only delegates to the
	     real <button class="ph"> elements inside, and keyboard is handled on
	     window, so the wrapper itself is presentational. -->
	<div
		class="host"
		role="presentation"
		bind:this={host}
		class:visible={phase === 'open'}
		style:left="{L.left}px"
		style:top="{L.top}px"
		style:width="{L.stageW}px"
		style:height="{L.pageH}px"
		onclick={onHostClick}
	></div>

	{#if loadError}
		<p class="err">{t('common.error')}</p>
	{/if}

	{#if phase === 'open'}
		<button class="nav prev icon-btn glass" aria-label={t('albums.prev')} title="{t('albums.prev')} (←)" onclick={() => flip?.flipPrev()}>
			<Icon name="left" size={22} />
		</button>
		{#if atEnd && photos.length}
			<button class="nav next icon-btn glass" aria-label={t('albums.toStart')} title="{t('albums.toStart')} (Home)" onclick={toStart}>
				<Icon name="first" size={22} />
			</button>
		{:else}
			<button class="nav next icon-btn glass" aria-label={t('albums.next')} title="{t('albums.next')} (→)" onclick={() => flip?.flipNext()}>
				<Icon name="right" size={22} />
			</button>
		{/if}
		<div class="foot">
			<span class="num">{t('albums.page', { a: photoNo, b: photos.length })}</span>
			{#if atEnd && photos.length}
				<span class="end-acts">
					<button class="end-btn glass" onclick={toStart}>
						<Icon name="first" size={14} />{t('albums.toStart')}
					</button>
					<button class="end-btn glass" onclick={close}>
						<Icon name="book" size={14} />{t('albums.backShelf')}
					</button>
				</span>
			{/if}
			<span class="hint">{t('albums.hint')}</span>
		</div>
	{/if}
	<button class="close icon-btn glass" aria-label={t('albums.close')} title="{t('albums.close')} (Esc)" onclick={close}>
		<Icon name="x" size={20} />
	</button>
</div>

{#if viewerIndex !== null}
	<Viewer
		{photos}
		index={viewerIndex}
		{rectOf}
		onindex={(i) => setHidden(i)}
		onclose={() => {
			viewerIndex = null;
			setHidden(null);
		}}
	/>
{/if}

<style>
	.book-overlay {
		position: fixed;
		inset: 0;
		z-index: 50;
	}
	.dim {
		position: absolute;
		inset: 0;
		width: 100%;
		background: color-mix(in oklch, var(--bg) 55%, transparent);
		backdrop-filter: blur(10px);
		-webkit-backdrop-filter: blur(10px);
		animation: fade 400ms var(--ease-soft);
		transition: opacity 500ms var(--ease-soft);
		cursor: zoom-out;
	}
	.closing .dim {
		opacity: 0;
	}
	.closed {
		position: fixed;
		transform-origin: 0 0;
		z-index: 2;
		filter: drop-shadow(0 30px 40px var(--shadow));
	}
	.closed.gone {
		visibility: hidden;
	}
	.host {
		position: fixed;
		z-index: 3;
		opacity: 0;
		pointer-events: none;
	}
	.host.visible {
		opacity: 1;
		pointer-events: auto;
	}

	/* ---- cover face (shared by the closed book and PageFlip's cover page) ---- */
	.book-overlay :global(.face) {
		position: relative;
		width: 100%;
		height: 100%;
		border-radius: 3px 10px 10px 3px;
		background:
			repeating-linear-gradient(45deg, rgb(255 255 255 / 0.05) 0 2px, transparent 2px 4px),
			repeating-linear-gradient(-45deg, rgb(0 0 0 / 0.05) 0 2px, transparent 2px 4px),
			linear-gradient(135deg, color-mix(in oklch, var(--cloth, var(--book-cloth)) 92%, #fff), var(--cloth, var(--book-cloth)));
		display: flex;
		flex-direction: column;
		align-items: center;
		padding: 16% 12% 0 16%;
		gap: 7%;
		overflow: hidden;
	}
	.book-overlay :global(.face-spine) {
		position: absolute;
		left: 0;
		top: 0;
		bottom: 0;
		width: 8%;
		background: linear-gradient(90deg, rgb(0 0 0 / 0.25), rgb(255 255 255 / 0.12) 60%, rgb(0 0 0 / 0.15));
	}
	.book-overlay :global(.face-photo) {
		position: relative;
		flex: none;
		width: 92%;
		aspect-ratio: 1;
		min-height: 0; /* see BookCover: tall photos must not stretch the box */
		max-height: 60%;
		background: var(--paper);
		box-shadow: 0 8px 18px -10px rgb(0 0 0 / 0.55);
		transform: rotate(-1.5deg);
	}
	.book-overlay :global(.face-photo img) {
		position: absolute;
		inset: 4%;
		width: 92%;
		height: 92%;
		object-fit: cover;
	}
	.book-overlay :global(.face-title) {
		flex: none;
		padding: 4px 14px;
		border-radius: 3px;
		background: color-mix(in oklch, var(--paper) 88%, transparent);
		color: #3b3a36;
		font-size: clamp(16px, 2.2vw, 24px);
		max-width: 100%;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.book-overlay :global(.face-date) {
		color: rgb(255 255 255 / 0.8);
		font-size: 13px;
		letter-spacing: 0.1em;
	}

	/* ---- pages (created imperatively, hence :global) ---- */
	.host :global(.pg) {
		background: var(--paper);
		overflow: hidden;
	}
	.host :global(.pg-paper) {
		background:
			radial-gradient(120% 90% at 50% 50%, transparent 60%, rgb(0 0 0 / 0.05)),
			repeating-linear-gradient(0deg, rgb(0 0 0 / 0.012) 0 1px, transparent 1px 3px),
			var(--paper);
		color: #4a4744;
	}
	/* PageFlip rewrites each page's inline style ("display:block; width;
	   height" in px) on every frame, so flex on .pg-paper itself is lost.
	   The centring box is an inner wrapper filling the page. */
	.host :global(.pg-in) {
		box-sizing: border-box;
		width: 100%;
		height: 100%;
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center; /* equal space above and below */
		gap: 14px;
		/* symmetric margins; the bottom one also holds the folio (page no.) */
		padding: max(30px, 6%) 0;
	}
	.host :global(.pg-hard) {
		background: var(--book-cloth);
	}
	.host :global(.pg-back) {
		background:
			repeating-linear-gradient(45deg, rgb(255 255 255 / 0.05) 0 2px, transparent 2px 4px),
			var(--book-cloth);
	}
	.host :global(.pg-hard .face) {
		--cloth: inherit;
	}
	.host :global(.intro-inner) {
		padding: 12%;
		text-align: center;
	}
	.host :global(.intro-inner h2) {
		margin: 0 0 8px;
		font-weight: 400;
		font-size: clamp(20px, 2.4vw, 30px);
	}
	.host :global(.intro-inner .date),
	.host :global(.intro-inner .count) {
		margin: 4px 0;
		font-size: 13px;
		color: #8c8580;
		letter-spacing: 0.08em;
	}
	.host :global(.intro-inner .desc) {
		margin: 18px 0;
		white-space: pre-line;
		line-height: 1.9;
		font-size: 15px;
	}
	.host :global(.fig) {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 8px;
		max-width: 100%;
	}
	.host :global(.pg-pair .pg-in) {
		gap: 18px;
	}
	.host :global(.pg-pair .cap) {
		font-size: 12px;
	}
	.host :global(button.ph) {
		padding: 7px 7px 7px;
		background: #fff;
		box-shadow:
			0 10px 18px -12px rgb(0 0 0 / 0.55),
			0 1px 2px rgb(0 0 0 / 0.08);
		transform: rotate(var(--r, 0deg));
		transition: transform 300ms var(--ease-soft);
		cursor: zoom-in;
	}
	.host :global(button.ph:hover) {
		transform: rotate(0deg) scale(1.015);
	}
	.host :global(button.ph img) {
		pointer-events: none; /* so PageFlip sees the <button> as the click target */
		display: block;
		object-fit: cover;
		/* the global img { max-width: 100% } must not squash the computed size */
		max-width: none;
	}
	.host :global(.cap) {
		font-family: var(--font-art);
		font-size: 13px;
		color: #8c8580;
		max-width: 80%;
		text-align: center;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.host :global(.folio) {
		position: absolute;
		left: 0;
		right: 0;
		bottom: 10px;
		text-align: center;
		font-size: 11px;
		color: #b0a9a2;
	}

	/* ---- controls ---- */
	.nav {
		position: fixed;
		top: 50%;
		z-index: 4;
		width: 48px;
		height: 48px;
		transform: translateY(-50%);
		animation: fade 500ms var(--ease-soft);
	}
	.prev {
		left: max(16px, 2vw);
	}
	.next {
		right: max(16px, 2vw);
	}
	.close {
		position: fixed;
		top: 14px;
		right: 16px;
		z-index: 5;
	}
	.foot {
		position: fixed;
		bottom: 18px;
		left: 0;
		right: 0;
		z-index: 4;
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: center;
		gap: 10px 14px;
		font-size: 13px;
		color: var(--text-muted);
		pointer-events: none;
	}
	.end-acts {
		display: inline-flex;
		gap: 8px;
		pointer-events: auto;
		animation: fade 400ms var(--ease-soft);
	}
	.end-btn {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		padding: 6px 14px;
		border-radius: 999px;
		font-size: 13px;
		color: var(--text);
	}
	.end-btn:hover {
		color: var(--accent);
	}
	.foot .hint {
		flex-basis: 100%;
		text-align: center;
		font-size: 12px;
		opacity: 0.75;
	}
	.err {
		position: fixed;
		top: 50%;
		width: 100%;
		text-align: center;
		z-index: 4;
	}
	@keyframes fade {
		from {
			opacity: 0;
		}
	}
	@media (max-width: 700px) {
		.nav {
			display: none; /* swipe on phones */
		}
		.foot .hint {
			display: none; /* no keyboard */
		}
	}
</style>


