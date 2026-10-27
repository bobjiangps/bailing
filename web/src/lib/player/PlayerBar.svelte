<script lang="ts">
	// Global bottom player bar. Present on every page once something is queued.
	import { base } from '$app/paths';
	import { coverFallback, coverUrl } from '$lib/api/client';
	import Icon from '$lib/components/Icon.svelte';
	import { t } from '$lib/i18n';
	import { prefs } from '$lib/stores/prefs.svelte';
	import { formatTime } from '$lib/util/format';
	import { player } from './player.svelte';
	import QueuePanel from './QueuePanel.svelte';
	import FxPanel from './FxPanel.svelte';
	import AddToPlaylist from '$lib/online/AddToPlaylist.svelte';
	import FavButton from '$lib/online/FavButton.svelte';
	import { toSaved } from '$lib/stores/collections.svelte';
	import TrafficChip from './TrafficChip.svelte';

	let queueOpen = $state(false);
	let fxOpen = $state(false);
	let seeking = $state<number | null>(null);

	const cur = $derived(player.current);
	const pos = $derived(seeking ?? player.time);
	const dur = $derived(player.duration || (cur?.duration_ms ?? 0) / 1000);
	const pct = $derived(dur > 0 ? Math.min(100, (pos / dur) * 100) : 0);
	const modeIcon = $derived(player.mode);

	function onKey(e: KeyboardEvent) {
		const tag = (e.target as HTMLElement)?.tagName;
		if (tag === 'INPUT' || tag === 'TEXTAREA' || (e.target as HTMLElement)?.isContentEditable) return;
		if (document.querySelector('.viewer')) return; // photo viewer owns the keyboard
		if (document.querySelector('dialog[open]')) return; // a modal dialog (e.g. download) owns it too
		if (!cur) return;
		switch (e.key) {
			case ' ':
				e.preventDefault();
				player.toggle();
				break;
			case 'ArrowRight':
				if (e.ctrlKey || e.metaKey) player.next();
				else if (!document.querySelector('.book-overlay')) player.seek(player.time + 5);
				break;
			case 'ArrowLeft':
				if (e.ctrlKey || e.metaKey) player.prev();
				else if (!document.querySelector('.book-overlay')) player.seek(player.time - 5);
				break;
			case 'ArrowUp':
				if (document.querySelector('.book-overlay')) return;
				e.preventDefault();
				player.setVolume(prefs.volume + 0.05);
				break;
			case 'ArrowDown':
				if (document.querySelector('.book-overlay')) return;
				e.preventDefault();
				player.setVolume(prefs.volume - 0.05);
				break;
			case 'm':
			case 'M':
				player.toggleMute();
				break;
		}
	}
</script>

<svelte:window onkeydown={onKey} />

{#if cur}
	<div class="bar glass" style:--tint={cur.color || 'var(--accent)'}>
		<div class="progress" class:live={player.live}>
			<input
				type="range"
				min="0"
				max={dur || 1}
				step="0.1"
				value={player.live ? 0 : pos}
				disabled={player.live}
				style:--fill="{player.live ? 0 : pct}%"
				aria-label="seek"
				oninput={(e) => (seeking = +e.currentTarget.value)}
				onchange={(e) => {
					player.seek(+e.currentTarget.value);
					seeking = null;
				}}
			/>
		</div>

		<a class="now" href="{base}/music" title={t('player.openFull')}>
			<span class="thumb" class:spin={player.playing}>
				{#if cur.cover}<img src={coverUrl(cur.cover)} alt="" use:coverFallback />{:else}<Icon name="disc" size={22} />{/if}
			</span>
			<span class="txt">
				<span class="title">{cur.title}</span>
				<span class="sub">
					{cur.artist || t('music.unknownArtist')}
					{#if player.error}<em class="err" title={player.errorDetail}> · {t(player.error)}{#if player.errorDetail}（{player.errorDetail}）{/if}</em>
					{:else if player.notice}<em class="note"> · {t(player.notice)}</em>{/if}
				</span>
			</span>
		</a>

		<div class="controls">
			<button class="icon-btn hide-s" title={t('player.mode.' + player.mode)} onclick={() => player.cycleMode()}>
				<Icon name={modeIcon} size={18} />
			</button>
			<button class="icon-btn" aria-label={t('player.prev')} onclick={() => player.prev()}>
				<Icon name="prev" size={18} />
			</button>
			<button class="icon-btn play" aria-label={player.playing ? t('player.pause') : t('player.play')} onclick={() => player.toggle()}>
				{#if player.loading}<span class="spinner"></span>{:else}<Icon name={player.playing ? 'pause' : 'play'} size={20} />{/if}
			</button>
			<button class="icon-btn" aria-label={t('player.next')} onclick={() => player.next()}>
				<Icon name="next" size={18} />
			</button>
		</div>

		<div class="right">
			{#if player.live}
				<span class="time live-tag hide-s">● {t('player.live')} · {formatTime(player.listened)}</span>
			{:else}
				<span class="time num hide-s">{formatTime(pos)} / {formatTime(dur)}</span>
			{/if}
			<span class="hide-s acts">
				<FavButton item={() => toSaved(cur)} />
				<AddToPlaylist items={() => [toSaved(cur)]} />
			</span>
			<span class="hide-s"><TrafficChip /></span>
			<span class="show-s"><TrafficChip compact /></span>
			<div class="vol hide-s">
				<button class="icon-btn" aria-label={t('player.mute')} onclick={() => player.toggleMute()}>
					<Icon name={player.muted || prefs.volume === 0 ? 'mute' : 'volume'} size={18} />
				</button>
				<input
					type="range"
					min="0"
					max="1"
					step="0.01"
					value={prefs.volume}
					style:--fill="{prefs.volume * 100}%"
					aria-label={t('player.volume')}
					oninput={(e) => player.setVolume(+e.currentTarget.value)}
				/>
			</div>
			<button
				class="icon-btn fx-btn"
				class:active={fxOpen}
				class:armed={player.sleepAt || player.sleepAfterTrack || prefs.eqOn || prefs.rate !== 1}
				title={t('fx.title')}
				aria-label={t('fx.title')}
				onclick={() => ((fxOpen = !fxOpen), (queueOpen = false))}
			>
				<Icon name="settings" size={18} />
				{#if player.sleepAt}<span class="sleep num">{Math.ceil(player.sleepLeft / 60)}′</span>{/if}
			</button>
			<button class="icon-btn" class:active={queueOpen} aria-label={t('music.queue')} onclick={() => ((queueOpen = !queueOpen), (fxOpen = false))}>
				<Icon name="queue" size={18} />
			</button>
		</div>
	</div>
	{#if queueOpen}<QueuePanel onclose={() => (queueOpen = false)} />{/if}
	{#if fxOpen}<FxPanel onclose={() => (fxOpen = false)} />{/if}
{/if}

<style>
	.bar {
		position: fixed;
		left: 12px;
		right: 12px;
		bottom: calc(10px + env(safe-area-inset-bottom));
		z-index: 35;
		height: calc(var(--playerbar-h) - 14px);
		display: flex;
		align-items: center;
		gap: 12px;
		padding: 0 12px 0 10px;
		border-radius: var(--radius-l);
		box-shadow: 0 20px 50px -28px var(--shadow);
		background: color-mix(in oklch, var(--surface-glass) 88%, var(--tint) 12%);
		transition: background 1.2s var(--ease-soft);
		animation: rise 500ms var(--ease-soft);
	}
	.progress {
		position: absolute;
		left: 18px;
		right: 18px;
		top: -8px;
	}
	.progress input {
		width: 100%;
	}
	.now {
		display: flex;
		align-items: center;
		gap: 10px;
		min-width: 0;
		flex: 1 1 0;
	}
	.thumb {
		width: 42px;
		height: 42px;
		flex: none;
		border-radius: 50%;
		overflow: hidden;
		display: grid;
		place-items: center;
		background: var(--accent-soft);
		box-shadow: 0 0 0 3px color-mix(in oklch, var(--text) 80%, transparent);
		animation: spin 22s linear infinite;
		animation-play-state: paused;
	}
	.thumb.spin {
		animation-play-state: running;
	}
	.thumb img {
		width: 100%;
		height: 100%;
		object-fit: cover;
	}
	.txt {
		display: flex;
		flex-direction: column;
		min-width: 0;
		line-height: 1.3;
	}
	.title,
	.sub {
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.title {
		font-size: 14px;
	}
	.sub {
		font-size: 12px;
		color: var(--text-muted);
	}
	.err {
		color: var(--accent);
		font-style: normal;
	}
	.note {
		color: var(--accent);
		font-style: normal;
		font-weight: 500;
	}
	.live-tag {
		color: var(--accent);
		letter-spacing: 0.04em;
	}
	.progress.live {
		opacity: 0.35;
		pointer-events: none;
	}
	.controls {
		display: flex;
		align-items: center;
		gap: 2px;
	}
	.play {
		width: 44px;
		height: 44px;
		background: var(--accent);
		color: var(--surface);
	}
	.play:hover {
		background: var(--accent);
		filter: brightness(1.05);
	}
	.right {
		flex: 1 1 0;
		display: flex;
		align-items: center;
		justify-content: flex-end;
		gap: 8px;
		min-width: 0;
	}
	.time {
		font-size: 12px;
		color: var(--text-muted);
		white-space: nowrap;
	}
	.acts {
		display: inline-flex;
	}
	.fx-btn {
		position: relative;
	}
	.fx-btn.armed {
		color: var(--accent);
	}
	.sleep {
		position: absolute;
		right: -2px;
		top: -4px;
		font-size: 9.5px;
		padding: 0 3px;
		border-radius: 6px;
		background: var(--accent);
		color: var(--surface);
	}
	.vol {
		display: flex;
		align-items: center;
	}
	.vol input {
		width: 84px;
	}
	.spinner {
		width: 18px;
		height: 18px;
		border-radius: 50%;
		border: 2px solid currentColor;
		border-right-color: transparent;
		animation: spin 0.9s linear infinite;
	}
	.show-s {
		display: none;
	}
	@keyframes spin {
		to {
			transform: rotate(360deg);
		}
	}
	@keyframes rise {
		from {
			opacity: 0;
			transform: translateY(20px);
		}
	}
	@media (max-width: 760px) {
		.hide-s {
			display: none;
		}
		.show-s {
			display: inline-flex;
		}
		.bar {
			left: 8px;
			right: 8px;
			gap: 6px;
		}
		.right {
			flex: none;
		}
	}
</style>
