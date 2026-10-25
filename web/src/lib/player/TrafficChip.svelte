<script lang="ts">
	// Server traffic of THIS opened page, summed over everything the server
	// sent (all tracks, relays, photos, covers, app) — not just the current
	// song. Restarts at zero when the page is reloaded / reopened.
	// Click to see the breakdown.
	import { base } from '$app/paths';
	import Icon from '$lib/components/Icon.svelte';
	import { t } from '$lib/i18n';
	import { pageTraffic } from '$lib/stores/pageTraffic.svelte';
	import { prefs } from '$lib/stores/prefs.svelte';
	import { formatBytes, formatTime } from '$lib/util/format';
	import { player } from './player.svelte';

	let { compact = false, drop = 'up' }: { compact?: boolean; drop?: 'up' | 'down' } = $props();

	let open = $state(false);
	let now = $state(Date.now());

	const cur = $derived(player.current);
	const kind = $derived(cur?.traffic ?? 'server');
	const level = $derived(pageTraffic.level);
	const tip = $derived(
		[
			t('traffic.pageTip', { b: formatBytes(pageTraffic.bytes) }),
			kind === 'proxy' ? t('traffic.tipProxy') : kind === 'server' && cur ? t('traffic.tipServer') : ''
		]
			.filter(Boolean)
			.join('\n')
	);
	const cats = $derived(
		Object.entries(pageTraffic.byCat)
			.filter(([, b]) => b > 0)
			.sort((a, b) => b[1] - a[1])
	);

	$effect(() => {
		if (!open) return;
		now = Date.now();
		pageTraffic.refresh();
		const id = setInterval(() => (now = Date.now()), 1000);
		return () => clearInterval(id);
	});

	function onWindowClick(e: MouseEvent) {
		if (open && !(e.target as HTMLElement).closest('.traffic-wrap')) open = false;
	}
</script>

<svelte:window onclick={onWindowClick} />

<span class="traffic-wrap">
	<button
		class="chip traffic {kind} {level}"
		class:compact
		class:playing={player.playing && kind !== 'direct'}
		title={tip}
		aria-expanded={open}
		onclick={() => (open = !open)}
	>
		<Icon name={kind === 'proxy' ? 'relay' : kind === 'direct' && cur ? 'zap' : 'server'} size={13} />
		{#if !compact}<span class="label">{t('traffic.thisPage')}</span>{/if}
		{#if cur && player.serverListened > 0}
			<span class="num">{formatTime(player.serverListened)}</span>
			<span class="sep">·</span>
		{/if}
		<span class="num">{formatBytes(pageTraffic.bytes)}</span>
	</button>

	{#if open}
		<div class="panel glass {drop}" role="dialog" aria-label={t('traffic.pageTitle')}>
			<div class="head">
				<span>{t('traffic.pageTitle')}</span>
				<strong class="num {level}">{formatBytes(pageTraffic.bytes)}</strong>
			</div>
			<ul>
				<li><span>{t('traffic.opened')}</span><span class="num">{formatTime((now - pageTraffic.openedAt) / 1000)}</span></li>
				<li><span>{t('traffic.serverPlayed')}</span><span class="num">{formatTime(player.serverListened)}</span></li>
				{#if cur}
					<li>
						<span>{t('traffic.current')} · {kind === 'proxy' ? t('traffic.proxy') : kind === 'direct' ? t('traffic.direct') : t('traffic.server')}</span>
						<span class="num">{kind === 'direct' ? '—' : formatBytes(player.streamBytes)}</span>
					</li>
				{/if}
			</ul>
			{#if cats.length}
				<ul class="cats">
					{#each cats as [c, b] (c)}
						<li><span>{t('cat.' + c)}</span><span class="num">{formatBytes(b)}</span></li>
					{/each}
				</ul>
			{/if}
			{#if level !== 'ok'}
				<p class="alert">{t(level === 'high' ? 'traffic.levelHigh' : 'traffic.levelWarn', { n: formatBytes(prefs.pageWarnMB * 1024 * 1024) })}</p>
			{/if}
			<p class="foot muted">
				{t('traffic.pageFoot')}
				<a href="{base}/settings#traffic" onclick={() => (open = false)}>{t('traffic.toSettings')}</a>
			</p>
		</div>
	{/if}
</span>

<style>
	.traffic-wrap {
		position: relative;
		display: inline-flex;
	}
	.traffic {
		font-size: 11.5px;
		gap: 5px;
		background: color-mix(in oklch, var(--surface) 70%, transparent);
		border: 1px solid var(--line);
		color: var(--text-muted);
		cursor: pointer;
		white-space: nowrap;
	}
	.traffic.direct {
		color: var(--accent-2);
	}
	.traffic.proxy.playing,
	.traffic.warn {
		/* relaying / over the per-page threshold: make it hard to miss */
		background: color-mix(in oklch, var(--accent) 22%, var(--surface));
		border-color: color-mix(in oklch, var(--accent) 45%, transparent);
		color: var(--text);
	}
	.traffic.high {
		background: var(--accent);
		color: var(--surface);
		border-color: transparent;
		animation: pulse 2.4s ease-in-out infinite;
	}
	.traffic.proxy.playing {
		animation: pulse 2.4s ease-in-out infinite;
	}
	.sep {
		opacity: 0.5;
	}
	.panel {
		position: absolute;
		right: 0;
		z-index: 40;
		width: 264px;
		padding: 12px 14px;
		border-radius: var(--radius-m);
		box-shadow: 0 18px 40px -20px var(--shadow);
		font-size: 13px;
		animation: pop var(--dur-base) var(--ease-soft);
	}
	.panel.up {
		bottom: calc(100% + 10px);
	}
	.panel.down {
		top: calc(100% + 10px);
	}
	.head {
		display: flex;
		align-items: baseline;
		justify-content: space-between;
		gap: 8px;
		margin-bottom: 6px;
	}
	.head strong {
		font-size: 20px;
		font-weight: 500;
	}
	.head strong.warn,
	.head strong.high {
		color: var(--accent);
	}
	ul {
		list-style: none;
		margin: 0;
		padding: 0;
	}
	li {
		display: flex;
		justify-content: space-between;
		gap: 10px;
		padding: 3px 0;
		border-bottom: 1px dashed var(--line);
	}
	.cats {
		margin-top: 8px;
		font-size: 12px;
		color: var(--text-muted);
	}
	.alert {
		margin: 8px 0 0;
		padding: 6px 8px;
		border-radius: 8px;
		background: color-mix(in oklch, var(--accent) 18%, transparent);
		font-size: 12.5px;
	}
	.foot {
		margin: 8px 0 0;
		font-size: 11.5px;
		line-height: 1.5;
	}
	.foot a {
		border-bottom: 1px dashed currentColor;
	}
	@keyframes pulse {
		50% {
			box-shadow: 0 0 0 5px color-mix(in oklch, var(--accent) 20%, transparent);
		}
	}
	@keyframes pop {
		from {
			opacity: 0;
			transform: translateY(6px);
		}
	}
</style>

