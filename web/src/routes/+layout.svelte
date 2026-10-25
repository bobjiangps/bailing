<script lang="ts">
	// Root layout: theme + session bootstrap, the scenery backdrop, the top
	// bar, the global player bar and the single <audio> element. Because the
	// audio element lives here, navigating between pages never stops the music.
	import '../app.css';
	import { onMount, untrack, type Snippet } from 'svelte';
	import { onNavigate } from '$app/navigation';
	import Backdrop from '$lib/components/Backdrop.svelte';
	import TopBar from '$lib/components/TopBar.svelte';
	import TrafficBanner from '$lib/components/TrafficBanner.svelte';
	import PlayerBar from '$lib/player/PlayerBar.svelte';
	import { player } from '$lib/player/player.svelte';
	import { prefs } from '$lib/stores/prefs.svelte';
	import { pageTraffic } from '$lib/stores/pageTraffic.svelte';
	import { session } from '$lib/stores/session.svelte';
	import { prefersReducedMotion } from '$lib/util/format';
	import { features } from '$lib/features.svelte';
	import { registerSW } from '$lib/pwa.svelte';
	import UpdateToast from '$lib/components/UpdateToast.svelte';

	let { children }: { children: Snippet } = $props();
	let audio: HTMLAudioElement | undefined = $state();

	onMount(() => {
		prefs.init();
		session.load();
		registerSW();
	});

	$effect(() => {
		if (audio) player.attach(audio);
	});

	// equaliser / spectrum switched on (possibly on another device): attach the graph
	$effect(() => {
		if (audio && (prefs.eqOn || prefs.spectrum)) untrack(() => player.enableFx(false));
	});

	// running total of server traffic for this opened page (members only)
	$effect(() => {
		if (!session.user) return;
		untrack(() => pageTraffic.start(() => player.playing && player.current?.traffic !== 'direct'));
		return () => pageTraffic.stop();
	});

	// Soft cross-fade between pages (View Transitions API, where supported).
	onNavigate((nav) => {
		if (!document.startViewTransition || prefersReducedMotion()) return;
		if (nav.from?.url.pathname === nav.to?.url.pathname) return;
		return new Promise((resolve) => {
			document.startViewTransition(async () => {
				resolve();
				await nav.complete;
			});
		});
	});
</script>

<Backdrop tint={player.current?.color ?? ''} />
<TopBar />
<TrafficBanner />

<main>
	{#if session.ready}
		{@render children()}
	{:else}
		<div class="boot" aria-hidden="true"><span></span></div>
	{/if}
</main>

{#if features.music}
	<PlayerBar />
	<!-- preload=none: never download audio before the user presses play -->
	<audio bind:this={audio} preload="none"></audio>
{/if}
<UpdateToast />

<style>
	main {
		/* No z-index here: it would make <main> a stacking context, and full-
		   screen overlays inside pages (open album z 50, photo viewer z 60)
		   could then never rise above the top bar (30) or player bar (35).
		   Painted after the fixed Backdrop (z 0) in tree order anyway. */
		position: relative;
		min-height: 100dvh;
	}
	.boot {
		display: grid;
		place-items: center;
		min-height: 100dvh;
	}
	.boot span {
		width: 10px;
		height: 10px;
		border-radius: 50%;
		background: var(--accent);
		animation: breathe 1.6s var(--ease-soft) infinite;
	}
	@keyframes breathe {
		50% {
			transform: scale(1.8);
			opacity: 0.4;
		}
	}
	:global(::view-transition-old(root)),
	:global(::view-transition-new(root)) {
		animation-duration: 380ms;
		animation-timing-function: cubic-bezier(0.22, 0.8, 0.24, 1);
	}
</style>

