<script lang="ts">
	// Scrolling lyrics: the active line is found by binary search on the
	// current time and the list is translated so it sits ~38% from the top.
	import { t } from '$lib/i18n';
	import { player } from './player.svelte';
	import LyricsSearch from './LyricsSearch.svelte';

	let box: HTMLDivElement | undefined = $state();
	let lineEls: HTMLElement[] = $state([]);
	let userScrolling = $state(false);
	let scrollTimer: ReturnType<typeof setTimeout> | undefined;

	const lines = $derived(player.lyrics?.lines ?? []);
	const synced = $derived(!!player.lyrics?.synced);

	const active = $derived.by(() => {
		if (!synced || !lines.length) return -1;
		const ms = player.time * 1000 + 200; // small lead so lines appear on time
		let lo = 0;
		let hi = lines.length - 1;
		let ans = -1;
		while (lo <= hi) {
			const mid = (lo + hi) >> 1;
			if (lines[mid].t <= ms) {
				ans = mid;
				lo = mid + 1;
			} else hi = mid - 1;
		}
		return ans;
	});

	$effect(() => {
		const i = active;
		if (!box || i < 0 || userScrolling) return;
		const el = lineEls[i];
		if (!el) return;
		const target = el.offsetTop - box.clientHeight * 0.38 + el.offsetHeight / 2;
		box.scrollTo({ top: target, behavior: 'smooth' });
	});

	function onWheel() {
		// let the user browse; resume auto-scroll after 3s
		userScrolling = true;
		clearTimeout(scrollTimer);
		scrollTimer = setTimeout(() => (userScrolling = false), 3000);
	}

	const sourceLabel = $derived(player.lyrics?.source ? t('lyrics.src.' + player.lyrics.source) : '');
	/** manual search is possible for any song with a title (not radio / live) */
	const canSearch = $derived(!!player.current && !player.current.live && player.current.source !== 'radio');
	let searchDlg: LyricsSearch | undefined = $state();
</script>

<div class="lyrics" bind:this={box}>
	{#if !player.current}
		<p class="hint art">{t('music.nothing')}</p>
	{:else if player.lyricsLoading}
		<p class="hint art">{t('lyrics.loading')}</p>
	{:else if player.lyrics?.source === 'radio'}
		<p class="hint art">{t('lyrics.radio')}</p>
	{:else if !player.lyrics || player.lyrics.status === 'none'}
		<div class="hint art">
			<div>
				<p>{t('lyrics.none')}</p>
				{#if canSearch}
					<button class="btn small-btn" onclick={() => searchDlg?.open()}>{t('lyrics.manual')}</button>
				{/if}
			</div>
		</div>
	{:else if player.lyrics.status === 'instrumental'}
		<p class="hint art">{t('lyrics.instrumental')}</p>
	{:else}
		<div class="pad"></div>
		{#each lines as line, i (i)}
			<button
				class="line art"
				class:on={i === active}
				class:past={synced && i < active}
				class:static={!synced}
				bind:this={lineEls[i]}
				onclick={() => synced && player.seek(line.t / 1000)}
			>
				<span class="main">{line.text || '♪'}</span>
				{#if line.trans}<span class="trans">{line.trans}</span>{/if}
			</button>
		{/each}
		<div class="pad"></div>
	{/if}
</div>
{#if player.lyrics && (player.lyrics.status === 'ok' || player.lyrics.status === 'instrumental') && canSearch}
	<div class="meta">
		{#if player.lyrics.status === 'ok'}<span>{sourceLabel}</span> · {/if}
		{#if player.lyrics.source === 'lrclib' && player.current?.source === 'local'}
			<button class="link" onclick={() => player.refetchLyrics()}>{t('lyrics.refetch')}</button> ·
		{/if}
		<button class="link" onclick={() => searchDlg?.open()}>{t('lyrics.manualOther')}</button>
		{#if player.lyrics.source === 'manual'}
			· <button class="link" onclick={() => player.resetLyrics()}>{t('lyrics.reset')}</button>
		{/if}
	</div>
{/if}
{#if canSearch}<LyricsSearch bind:this={searchDlg} />{/if}

<style>
	.lyrics {
		position: relative;
		height: 100%;
		overflow-y: auto;
		scrollbar-width: none;
		mask-image: linear-gradient(to bottom, transparent, #000 18%, #000 78%, transparent);
		-webkit-mask-image: linear-gradient(to bottom, transparent, #000 18%, #000 78%, transparent);
	}
	.lyrics::-webkit-scrollbar {
		display: none;
	}
	.pad {
		height: 40%;
	}
	.line {
		display: flex;
		flex-direction: column;
		align-items: center;
		width: 100%;
		padding: 9px 12px;
		text-align: center;
		color: var(--text-muted);
		opacity: 0.75;
		transition:
			color 500ms var(--ease-soft),
			opacity 500ms var(--ease-soft),
			transform 500ms var(--ease-soft);
	}
	.line.static {
		cursor: default;
		opacity: 0.9;
	}
	.line .main {
		font-size: 18px;
		line-height: 1.5;
	}
	.line .trans {
		font-size: 14px;
		opacity: 0.8;
		margin-top: 2px;
	}
	.line.past {
		opacity: 0.5;
	}
	.line.on {
		color: var(--text);
		opacity: 1;
		transform: scale(1.08);
	}
	.line:not(.static):hover {
		opacity: 1;
	}
	.hint {
		display: grid;
		place-items: center;
		height: 100%;
		margin: 0;
		color: var(--text-muted);
		font-size: 18px;
		text-align: center;
		padding: 0 24px;
	}
	.meta {
		text-align: center;
		font-size: 12px;
		color: var(--text-muted);
		padding-top: 6px;
	}
	.link {
		text-decoration: underline;
		text-underline-offset: 3px;
	}
	.hint p {
		margin: 0;
	}
	.small-btn {
		margin-top: 14px;
		font-size: 13px;
	}
</style>
