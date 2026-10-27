<script lang="ts">
	// Manual lyrics search: the automatic match uses the title / artist from
	// the tags (or guessed from the file name). When that is wrong the user
	// types better keywords here, picks a result, and the choice is saved on
	// the server (local songs: also written as .lrc beside the file).
	import { t } from '$lib/i18n';
	import { ApiError, type LyricsCandidate } from '$lib/api/client';
	import { formatTime } from '$lib/util/format';
	import Icon from '$lib/components/Icon.svelte';
	import { player } from './player.svelte';

	let dlg: HTMLDialogElement | undefined = $state();
	let title = $state('');
	let artist = $state('');
	let album = $state('');
	let results = $state<LyricsCandidate[] | null>(null);
	let searching = $state(false);
	let picking = $state(0);
	let err = $state('');

	const curDur = $derived(player.current?.duration_ms || 0);

	export function open() {
		const cur = player.current;
		if (!cur || !dlg) return;
		title = cur.title || '';
		artist = cur.artist || '';
		album = '';
		results = null;
		err = '';
		dlg.showModal();
	}

	function errText(e: unknown) {
		if (e instanceof ApiError && e.code === 'lyrics_offline') return t('lyrics.offline');
		if (e instanceof ApiError && e.code === 'not_found') return t('lyrics.pickGone');
		return t('lyrics.searchFail');
	}

	async function search(e?: Event) {
		e?.preventDefault();
		if (!title.trim() && !artist.trim()) return;
		searching = true;
		err = '';
		try {
			results = await player.searchLyrics({ title, artist, album });
		} catch (x) {
			results = null;
			err = errText(x);
		} finally {
			searching = false;
		}
	}

	async function pick(c: LyricsCandidate) {
		picking = c.id;
		err = '';
		try {
			await player.pickLyrics(c.id);
			dlg?.close();
		} catch (x) {
			err = errText(x);
		} finally {
			picking = 0;
		}
	}

	/** duration difference to the playing song, e.g. "+3s" */
	function delta(c: LyricsCandidate) {
		if (!curDur || !c.duration_ms) return '';
		const d = Math.round((c.duration_ms - curDur) / 1000);
		return d === 0 ? '±0s' : `${d > 0 ? '+' : ''}${d}s`;
	}
	const near = (c: LyricsCandidate) => !!curDur && !!c.duration_ms && Math.abs(c.duration_ms - curDur) <= 5000;

	function onDialogClick(e: MouseEvent) {
		if (e.target === dlg) dlg?.close();
	}
</script>

<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_noninteractive_element_interactions -->
<dialog bind:this={dlg} class="ls-dialog" aria-label={t('lyrics.searchTitle')} onclick={onDialogClick}>
	<form onsubmit={search}>
		<h3><Icon name="search" size={17} />{t('lyrics.searchTitle')}</h3>
		<p class="muted small">{t('lyrics.searchHint')}</p>
		<div class="grid">
			<label>
				<span class="k">{t('lyrics.kTitle')}</span>
				<input class="field" type="text" bind:value={title} maxlength="200" />
			</label>
			<label>
				<span class="k">{t('lyrics.kArtist')}</span>
				<input class="field" type="text" bind:value={artist} maxlength="200" />
			</label>
			<label>
				<span class="k">{t('lyrics.kAlbum')}</span>
				<input class="field" type="text" bind:value={album} maxlength="200" placeholder={t('lyrics.optional')} />
			</label>
		</div>
		<div class="btns">
			<button type="button" class="btn" onclick={() => dlg?.close()}>{t('common.cancel')}</button>
			<button type="submit" class="btn primary" disabled={searching || (!title.trim() && !artist.trim())}>
				<Icon name="search" size={14} />{searching ? t('lyrics.searching') : t('common.search')}
			</button>
		</div>
	</form>

	{#if err}
		<p class="small bad" role="alert">{err}</p>
	{/if}

	{#if results}
		{#if !results.length}
			<p class="muted small">{t('lyrics.noResults')}</p>
		{:else}
			<p class="muted small">{t('lyrics.resultHint', { n: results.length })}</p>
			<ul class="results">
				{#each results as c (c.id)}
					<li>
						<div class="info">
							<strong>{c.title}</strong>
							<span class="muted sub">
								{c.artist}{#if c.album} · {c.album}{/if}
							</span>
							<span class="tags small">
								{#if c.duration_ms}
									<span class="num" class:near={near(c)}>{formatTime(c.duration_ms / 1000)}{#if delta(c)} ({delta(c)}){/if}</span>
								{/if}
								<span class="chip">
									{c.instrumental ? t('lyrics.tagInstr') : c.synced ? t('lyrics.tagSynced') : t('lyrics.tagPlain')}
								</span>
							</span>
							{#if c.preview}<span class="preview small muted">{c.preview}</span>{/if}
						</div>
						<button class="btn" disabled={picking !== 0} onclick={() => pick(c)}>
							{picking === c.id ? t('lyrics.picking') : t('lyrics.use')}
						</button>
					</li>
				{/each}
			</ul>
		{/if}
	{/if}
</dialog>

<style>
	.ls-dialog {
		width: min(560px, calc(100vw - 32px));
		max-height: min(80vh, 720px);
		padding: 20px 22px 18px;
		border: 1px solid var(--line);
		border-radius: var(--radius-l, 18px);
		color: var(--text);
		background: var(--surface);
		box-shadow: 0 30px 80px -30px var(--shadow);
		text-align: left;
		overflow-y: auto;
	}
	.ls-dialog::backdrop {
		background: rgb(0 0 0 / 0.35);
		backdrop-filter: blur(2px);
	}
	h3 {
		display: flex;
		align-items: center;
		gap: 8px;
		margin: 0 0 6px;
		font-size: 16px;
		font-weight: 500;
	}
	.small {
		font-size: 12px;
		line-height: 1.5;
	}
	.grid {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: 10px;
		margin: 12px 0;
	}
	.grid label {
		display: flex;
		flex-direction: column;
		gap: 4px;
		min-width: 0;
	}
	.grid label:last-child {
		grid-column: 1 / -1;
	}
	.k {
		font-size: 12px;
		color: var(--text-muted);
	}
	.field {
		padding: 8px 12px;
		border-radius: 12px;
		border: 1px solid var(--line);
		background: var(--surface);
		color: var(--text);
		font-size: 14px;
		min-width: 0;
	}
	.btns {
		display: flex;
		justify-content: flex-end;
		gap: 8px;
	}
	.bad {
		color: var(--accent);
	}
	.results {
		list-style: none;
		margin: 6px 0 0;
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: 6px;
	}
	.results li {
		display: flex;
		align-items: center;
		gap: 10px;
		padding: 10px 12px;
		border: 1px solid var(--line);
		border-radius: 12px;
	}
	.info {
		display: flex;
		flex-direction: column;
		gap: 2px;
		flex: 1;
		min-width: 0;
	}
	.info strong {
		font-weight: 500;
		overflow-wrap: anywhere;
	}
	.sub {
		font-size: 13px;
		overflow-wrap: anywhere;
	}
	.tags {
		display: flex;
		align-items: center;
		gap: 8px;
		flex-wrap: wrap;
	}
	.near {
		color: var(--accent);
	}
	.preview {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	@media (max-width: 520px) {
		.grid {
			grid-template-columns: 1fr;
		}
	}
</style>


