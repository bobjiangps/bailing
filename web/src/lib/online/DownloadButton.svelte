<script lang="ts">
	// Saves a song to this device (see player/download.ts). Online (LX)
	// songs first show a small confirmation: what is saved, in which quality
	// and how big (as the platform lists it), with the quality changeable
	// right there. Only after "Download" is the real file checked: when it is
	// not that quality (an error body of a few bytes, far from the listed
	// size, a lower quality) nothing is saved and the dialog says so.
	// Every source script tried is listed live ("source A: the 128k file is
	// broken → switching to source B → downloading from source B"), so it is
	// always clear where the file comes from.
	import Icon from '$lib/components/Icon.svelte';
	import { t } from '$lib/i18n';
	import {
		BadQualityError,
		canDownload,
		defaultQuality,
		download,
		hasQualities,
		qualityOptions,
		type QualityOption,
		type SourceStep
	} from '$lib/player/download';
	import type { QueueItem } from '$lib/player/player.svelte';
	import { prefs } from '$lib/stores/prefs.svelte';
	import { formatBytes } from '$lib/util/format';

	let { item, size = 16 }: { item: () => QueueItem; size?: number } = $props();

	let busy = $state(false);
	let err = $state('');
	let note = $state('');
	let timer: ReturnType<typeof setTimeout> | undefined;

	// ---- confirmation dialog ----
	let dlg: HTMLDialogElement | undefined = $state();
	let cur = $state<QueueItem | null>(null);
	let opts = $state<QualityOption[]>([]);
	let quality = $state('');
	let dontAsk = $state(false);
	/** fetching the real file of the chosen quality */
	let fetching = $state(false);
	/** why the chosen quality was refused (shown in the dialog) */
	let dlgErr = $state('');
	/** qualities found broken for this song while the dialog is open */
	let broken = $state<string[]>([]);
	/** live log of the source scripts tried for the current attempt */
	let steps = $state<SourceStep[]>([]);
	/** the download started; the file comes from this source script */
	let doneFrom = $state<string | null>(null);
	let abort: AbortController | null = null;
	const picked = $derived(opts.find((o) => o.q === quality));
	const switched = $derived(steps.some((s) => s.status === 'bad'));

	/** a "trying" line is replaced by that script's result */
	function addStep(s: SourceStep) {
		const last = steps.at(-1);
		if (s.status !== 'trying' && last?.status === 'trying' && last.script === s.script) {
			steps = [...steps.slice(0, -1), s];
		} else {
			steps = [...steps, s];
		}
	}

	/** the server's (English) reason for a broken link, in short words */
	function reason(d = ''): string {
		let m: RegExpMatchArray | null;
		if ((m = d.match(/HTTP (\d+)/))) return t('dl.r.http', { n: m[1] });
		if (d.includes('web page')) return t('dl.r.page');
		if ((m = d.match(/only (\d+) bytes/))) return t('dl.r.tiny', { b: formatBytes(+m[1]) });
		if ((m = d.match(/\(([^()]+) instead of audio\)/))) return t('dl.r.type', { t: m[1] });
		if (d.includes('bad link')) return t('dl.r.badLink');
		if (/timed out|deadline|timeout/i.test(d)) return t('dl.r.timeout');
		return d;
	}

	function stepText(s: SourceStep, i: number): string {
		const name = s.script || '?';
		const v = { s: name, q: s.quality };
		switch (s.status) {
			case 'trying':
				return t(steps.slice(0, i).some((p) => p.status === 'bad') ? 'dl.s.switch' : 'dl.s.try', v);
			case 'ok': {
				let txt = t(switched ? 'dl.s.okSwitched' : 'dl.s.ok', v);
				if (s.bytes) txt += ` · ${formatBytes(s.bytes)}`;
				if (s.cached) txt += t('dl.s.cached');
				return txt;
			}
		}
		const b = s.bytes ? formatBytes(s.bytes) : '?';
		const e = s.expected ? formatBytes(s.expected) : '?';
		switch (s.kind) {
			case 'no_quality':
				return t('dl.s.noQuality', v);
			case 'dead':
				return t('dl.s.dead', { ...v, r: reason(s.detail) });
			case 'lower':
				return t('dl.s.lower', { ...v, g: s.got ?? '?', b });
			case 'mismatch':
				return t('dl.s.mismatch', { ...v, b, e }) + (s.got ? t('dl.s.looksLike', { g: s.got }) : '');
		}
		return t('dl.s.error', { ...v, r: reason(s.detail) });
	}

	function onClick() {
		if (busy) return;
		const it = item();
		if (!hasQualities(it) || !prefs.dlAsk) return start(it);
		cur = it;
		opts = qualityOptions(it);
		quality = defaultQuality(opts, prefs.lxQuality);
		dontAsk = false;
		dlgErr = '';
		broken = [];
		steps = [];
		doneFrom = null;
		dlg?.showModal();
	}

	async function confirm(e: SubmitEvent) {
		e.preventDefault();
		if (doneFrom !== null) return closeDialog();
		if (!cur || fetching || !quality) return;
		const it = cur;
		const q = quality;
		fetching = true;
		dlgErr = '';
		steps = [];
		abort = new AbortController();
		try {
			const r = await download(it, q, true, abort.signal, addStep);
			if (dontAsk) prefs.set('dlAsk', false);
			// stays open: the log shows where the file comes from
			doneFrom = r.script ?? '';
		} catch (e) {
			if (abort?.signal.aborted) return; // cancelled
			if (e instanceof BadQualityError) {
				if (!broken.includes(q)) broken = [...broken, q];
				const s = opts.find((o) => o.q === q)?.size;
				dlgErr = s ? t('dl.bad', { q, s }) : t('dl.badNoSize', { q });
			} else if (steps.some((s) => s.status === 'bad')) {
				dlgErr = t('dl.allFailed', { q });
			} else {
				dlgErr = t('music.downloadFail', { e: e instanceof Error ? e.message : String(e) });
			}
		} finally {
			fetching = false;
			abort = null;
		}
	}

	async function start(it: QueueItem, q?: string) {
		busy = true;
		err = '';
		note = '';
		clearTimeout(timer);
		try {
			const r = await download(it, q);
			const notes: string[] = [];
			if (r.script) {
				notes.push(
					r.failed?.length
						? t('music.dlFromSwitched', { f: r.failed.join('、'), s: r.script })
						: t('music.dlFrom', { s: r.script })
				);
			}
			if (r.lower) notes.push(t('music.dlQuality', { q: r.lower }));
			if (notes.length) {
				note = notes.join('；');
				timer = setTimeout(() => (note = ''), 8000);
			}
		} catch (e) {
			err = t('music.downloadFail', { e: e instanceof Error ? e.message : String(e) });
			timer = setTimeout(() => (err = ''), 6000);
		} finally {
			busy = false;
		}
	}

	function closeDialog() {
		abort?.abort(); // a pending check must not save a file after "Cancel"
		dlg?.close();
	}

	/** a click on the dimmed backdrop (outside the box) closes the dialog */
	function onDialogClick(e: MouseEvent) {
		if (e.target === dlg) closeDialog();
	}
</script>

{#if canDownload(item())}
	<span class="wrap">
		<button
			class="icon-btn dl"
			class:busy
			class:bad={!!err}
			title={err || t('music.download')}
			aria-label={t('music.download')}
			disabled={busy}
			onclick={onClick}
		>
			<Icon name="download" {size} />
		</button>
		{#if err}<span class="tip" role="alert">{err}</span>{:else if note}<span class="tip" role="status">{note}</span>{/if}
	</span>
{/if}

<!-- only online (LX) rows get a dialog: local lists can have thousands of rows -->
{#if hasQualities(item())}
<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_noninteractive_element_interactions -->
<dialog
	bind:this={dlg}
	class="dl-dialog"
	aria-label={t('dl.title')}
	onclick={onDialogClick}
	oncancel={() => abort?.abort()}
	onclose={() => (cur = null)}
>
	{#if cur}
		<form onsubmit={confirm}>
			<h3><Icon name="download" size={17} />{t('dl.title')}</h3>
			<div class="song">
				<strong class="name">{cur.title}</strong>
				<span class="muted sub">
					{cur.artist || t('music.unknownArtist')}{#if cur.album} · {cur.album}{/if}
					<span class="plat">{cur.source.slice(3).toUpperCase()}</span>
				</span>
			</div>
			<label class="field">
				<span class="k">{t('online.quality')}</span>
				<select bind:value={quality} disabled={fetching || doneFrom !== null} onchange={() => ((dlgErr = ''), (steps = []))}>
					{#each opts as o (o.q)}
						<option value={o.q}>{o.q}{o.size ? ` · ${o.size}` : ''}{broken.includes(o.q) ? ` · ${t('dl.brokenTag')}` : ''}</option>
					{/each}
				</select>
			</label>
			<div class="field">
				<span class="k">{t('dl.size')}</span>
				<span class="num">{!picked ? '—' : picked.size ? t('dl.about', { s: picked.size }) : t('dl.sizeUnknown')}</span>
			</div>
			{#if steps.length}
				<ol class="steps small" aria-live="polite">
					{#each steps as s, i (i)}
						<li class={s.status}>
							<span class="ic" aria-hidden="true">
								{#if s.status === 'trying'}<span class="spin"></span>{:else if s.status === 'ok'}✓{:else}✗{/if}
							</span>
							<span>{stepText(s, i)}</span>
						</li>
					{/each}
				</ol>
			{:else if fetching}
				<p class="small status" role="status"><span class="spin"></span>{t('dl.fetching', { q: quality })}</p>
			{/if}
			{#if doneFrom !== null}
				<p class="small ok-msg" role="status">{doneFrom ? t('dl.done', { s: doneFrom }) : ''}</p>
			{:else if dlgErr}
				<div class="small bad" role="alert"><p>{dlgErr}</p></div>
			{:else if !steps.length && !fetching}
				<p class="muted small">{t('dl.hint')}</p>
			{/if}
			{#if doneFrom === null}
				<label class="ask small">
					<input type="checkbox" bind:checked={dontAsk} />{t('dl.dontAsk')}
				</label>
			{/if}
			<div class="btns">
				{#if doneFrom !== null}
					<!-- svelte-ignore a11y_autofocus -->
					<button type="submit" class="btn primary" autofocus>{t('dl.close')}</button>
				{:else}
					<button type="button" class="btn" onclick={closeDialog}>{t('common.cancel')}</button>
					<!-- svelte-ignore a11y_autofocus -->
					<button type="submit" class="btn primary" autofocus disabled={fetching || !quality || broken.includes(quality)}>
						<Icon name="download" size={14} />{t('dl.go')}
					</button>
				{/if}
			</div>
		</form>
	{/if}
</dialog>
{/if}

<style>
	.wrap {
		position: relative;
		display: inline-flex;
	}
	.dl {
		width: 32px;
		height: 32px;
		color: var(--text-muted);
	}
	.dl.busy {
		opacity: 0.5;
	}
	.dl.bad {
		color: var(--accent);
	}
	.tip {
		position: absolute;
		right: 0;
		bottom: calc(100% + 4px);
		z-index: 5;
		width: max-content;
		max-width: 280px;
		padding: 6px 10px;
		border-radius: 10px;
		font-size: 12px;
		line-height: 1.5;
		background: var(--surface);
		border: 1px solid var(--line);
		box-shadow: 0 10px 24px -14px var(--shadow);
	}

	/* native modal <dialog>: top layer (above the drawer and the player bar),
	   focus stays inside, Esc closes */
	.dl-dialog {
		width: min(400px, calc(100vw - 32px));
		padding: 20px 22px 18px;
		border: 1px solid var(--line);
		border-radius: var(--radius-l, 18px);
		color: var(--text);
		background: var(--surface);
		box-shadow: 0 30px 80px -30px var(--shadow);
		text-align: left;
	}
	.dl-dialog::backdrop {
		background: rgb(0 0 0 / 0.35);
		backdrop-filter: blur(2px);
	}
	.dl-dialog[open] {
		animation: pop var(--dur-base, 200ms) var(--ease-soft, ease-out);
	}
	@keyframes pop {
		from {
			opacity: 0;
			transform: translateY(8px) scale(0.98);
		}
	}
	@media (prefers-reduced-motion: reduce) {
		.dl-dialog[open] {
			animation: none;
		}
	}
	h3 {
		display: flex;
		align-items: center;
		gap: 8px;
		margin: 0 0 14px;
		font-size: 16px;
		font-weight: 500;
	}
	.song {
		display: flex;
		flex-direction: column;
		gap: 2px;
		margin-bottom: 14px;
		min-width: 0;
	}
	.name {
		font-size: 17px;
		font-weight: 500;
		overflow-wrap: anywhere;
	}
	.sub {
		font-size: 13px;
		overflow-wrap: anywhere;
	}
	.plat {
		margin-left: 6px;
		padding: 0 6px;
		border: 1px solid var(--line);
		border-radius: 4px;
		font-size: 11px;
		letter-spacing: 0.05em;
	}
	.field {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 12px;
		padding: 8px 0;
		border-top: 1px dashed var(--line);
		font-size: 14px;
	}
	.field .k {
		color: var(--text-muted);
	}
	.status {
		display: flex;
		align-items: center;
		gap: 8px;
		margin: 10px 0 6px;
	}
	.spin {
		width: 13px;
		height: 13px;
		flex: none;
		border-radius: 50%;
		border: 2px solid var(--line);
		border-top-color: var(--accent);
		animation: spin 0.8s linear infinite;
	}
	@keyframes spin {
		to {
			transform: rotate(360deg);
		}
	}
	.bad {
		margin: 10px 0 6px;
		padding: 8px 10px;
		border-radius: 10px;
		border: 1px solid color-mix(in srgb, var(--accent) 45%, transparent);
		background: color-mix(in srgb, var(--accent) 8%, transparent);
	}
	.bad p {
		margin: 0;
	}
	/* live log of the source scripts tried */
	.steps {
		list-style: none;
		margin: 10px 0 6px;
		padding: 8px 10px;
		border-radius: 10px;
		border: 1px solid var(--line);
		display: flex;
		flex-direction: column;
		gap: 4px;
	}
	.steps li {
		display: flex;
		align-items: flex-start;
		gap: 8px;
		overflow-wrap: anywhere;
	}
	.steps .ic {
		flex: none;
		width: 14px;
		display: inline-flex;
		justify-content: center;
		padding-top: 3px;
		line-height: 1;
	}
	.steps li.trying {
		color: var(--text-muted);
	}
	.steps li.bad .ic {
		color: var(--accent);
	}
	.steps li.ok {
		font-weight: 500;
	}
	.steps li.ok .ic {
		color: var(--accent-2, var(--accent));
	}
	.ok-msg {
		color: var(--text-muted);
	}
	select {
		min-width: 0;
		padding: 4px 10px;
		border-radius: 8px;
		border: 1px solid var(--line);
		background: var(--surface);
		color: var(--text);
		font: inherit;
	}
	.small {
		font-size: 12.5px;
		line-height: 1.6;
	}
	p.small {
		margin: 10px 0 6px;
	}
	.ask {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		color: var(--text-muted);
		cursor: pointer;
	}
	.btns {
		display: flex;
		justify-content: flex-end;
		gap: 8px;
		margin-top: 14px;
	}
</style>

