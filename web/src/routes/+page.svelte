<script lang="ts">
	// Home: a quiet greeting, two doors (albums / music) and — for members
	// only — the small "sticky notes" strip with recent / most played & viewed.
	import { base } from '$app/paths';
	import { api } from '$lib/api/client';
	import Icon from '$lib/components/Icon.svelte';
	import StatsNotes from '$lib/components/StatsNotes.svelte';
	import { t, tl } from '$lib/i18n';
	import { session } from '$lib/stores/session.svelte';
	import { features } from '$lib/features.svelte';

	type Home = { counts?: { albums: number; photos: number; tracks: number } };
	let home = $state<Home>({});

	$effect(() => {
		void session.user; // reload when the login state changes
		api<Home>('/api/home')
			.then((r) => (home = r))
			.catch(() => {});
	});

	function greetKey(h: number) {
		if (h < 5) return 'greet.night';
		if (h < 11) return 'greet.morning';
		if (h < 13) return 'greet.noon';
		if (h < 17) return 'greet.afternoon';
		if (h < 20) return 'greet.evening';
		return 'greet.night';
	}
	const greet = $derived(t(greetKey(new Date().getHours())));
	const lines = $derived(tl('home.lines'));
	// pick one line per day so it feels like a daily note, not random noise
	const line = $derived(lines.length ? lines[Math.floor(Date.now() / 86400000) % lines.length] : '');

	const doorHref = (p: string) => (session.user ? `${base}${p}` : `${base}/login?next=${encodeURIComponent(base + p)}`);
</script>

<svelte:head><title>{t('brand')} · Bailing</title></svelte:head>

<div class="page home">
	<section class="hero">
		<h1 class="art">
			{greet}{#if session.user}，{session.user.display_name || session.user.name}{/if}
		</h1>
		<p class="line art muted">{line}</p>
	</section>

	<section class="doors">
		{#if features.photos}
		<a class="door albums" href={doorHref('/albums')}>
			<span class="art-deco" aria-hidden="true">
				<i class="b b1"></i><i class="b b2"></i><i class="b b3"></i>
			</span>
			<span class="label">
				<span class="name art">{t('home.albums')}</span>
				<span class="sub muted">{t('home.albumsSub')}</span>
				{#if home.counts}
					<span class="chip num">{t('albums.photos', { n: home.counts.photos })}</span>
				{/if}
			</span>
			<Icon name="right" size={18} />
		</a>
		{/if}

		{#if features.music}
		<a class="door music" href={doorHref('/music')}>
			<span class="art-deco" aria-hidden="true">
				<i class="disc"></i>
			</span>
			<span class="label">
				<span class="name art">{t('home.music')}</span>
				<span class="sub muted">{t('home.musicSub')}</span>
				{#if home.counts}
					<span class="chip num">{t('music.tracks', { n: home.counts.tracks })}</span>
				{/if}
			</span>
			<Icon name="right" size={18} />
		</a>
		{/if}
	</section>

	{#if session.user}
		<StatsNotes />
	{:else}
		<p class="hint muted">
			<a href="{base}/login">{t('home.loginHint')}</a>
		</p>
	{/if}
</div>

<style>
	.home {
		display: flex;
		flex-direction: column;
		gap: clamp(28px, 6vh, 64px);
		min-height: 100dvh;
		justify-content: center;
		max-width: 920px;
	}
	.hero {
		animation: rise 900ms var(--ease-soft) both;
	}
	h1 {
		margin: 0;
		font-size: clamp(30px, 5.4vw, 52px);
		font-weight: 400;
		line-height: 1.25;
	}
	.line {
		margin: 12px 0 0;
		font-size: clamp(15px, 2vw, 19px);
	}

	.doors {
		display: grid;
		grid-template-columns: repeat(2, minmax(0, 1fr));
		gap: 22px;
	}
	@media (max-width: 640px) {
		.doors {
			grid-template-columns: 1fr;
		}
	}
	.door {
		position: relative;
		display: flex;
		align-items: center;
		gap: 18px;
		padding: 26px 24px;
		border-radius: var(--radius-l);
		background: color-mix(in oklch, var(--surface) 72%, transparent);
		border: 1px solid var(--line);
		box-shadow: 0 18px 50px -34px var(--shadow);
		overflow: hidden;
		animation: rise 900ms var(--ease-soft) both;
		transition:
			transform var(--dur-base) var(--ease-soft),
			box-shadow var(--dur-base) var(--ease-soft);
	}
	.door.music {
		animation-delay: 120ms;
	}
	.door.albums {
		animation-delay: 60ms;
	}
	.door:hover {
		transform: translateY(-4px) rotate(-0.3deg);
		box-shadow: 0 26px 60px -34px var(--shadow);
	}
	.label {
		flex: 1;
		display: flex;
		flex-direction: column;
		align-items: flex-start;
		gap: 4px;
	}
	.name {
		font-size: 24px;
	}
	.sub {
		font-size: 13.5px;
	}
	.chip {
		margin-top: 6px;
	}

	/* decorations: three leaning books / a slowly turning record */
	.art-deco {
		position: relative;
		width: 84px;
		height: 84px;
		flex: none;
	}
	.b {
		position: absolute;
		bottom: 6px;
		width: 22px;
		border-radius: 3px 5px 5px 3px;
		box-shadow: inset 3px 0 0 rgb(0 0 0 / 0.12);
		transform-origin: bottom left;
		transition: transform var(--dur-slow) var(--ease-soft);
	}
	.b1 {
		left: 8px;
		height: 66px;
		background: var(--accent);
	}
	.b2 {
		left: 32px;
		height: 74px;
		background: var(--accent-2);
	}
	.b3 {
		left: 56px;
		height: 60px;
		background: var(--book-cloth);
		transform: rotate(9deg);
	}
	.door:hover .b2 {
		transform: translateY(-6px);
	}
	.door:hover .b3 {
		transform: rotate(14deg);
	}
	.disc {
		position: absolute;
		inset: 4px;
		border-radius: 50%;
		background:
			radial-gradient(circle, var(--accent) 0 17%, var(--surface) 17% 20%, transparent 21%),
			repeating-radial-gradient(circle, #222 0 2px, #2c2c2c 2px 3px);
		box-shadow: 0 10px 24px -12px var(--shadow);
		animation: spin 14s linear infinite;
		animation-play-state: paused;
	}
	.door:hover .disc {
		animation-play-state: running;
	}

	.hint {
		text-align: center;
		font-size: 14px;
	}
	.hint a {
		border-bottom: 1px dashed currentColor;
	}

	@keyframes rise {
		from {
			opacity: 0;
			transform: translateY(16px);
		}
	}
	@keyframes spin {
		to {
			transform: rotate(360deg);
		}
	}
</style>

