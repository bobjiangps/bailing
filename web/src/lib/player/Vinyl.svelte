<script lang="ts">
	// A record that spins while playing and keeps its angle when paused
	// (animation-play-state), with a tone arm that lifts / drops.
	import { coverFallback, coverUrl } from '$lib/api/client';
	import { player } from './player.svelte';

	let { size = 320 }: { size?: number } = $props();
	const cover = $derived(player.current?.cover ? coverUrl(player.current.cover, 'm') : '');
</script>

<div class="turntable" style:--size="{size}px">
	<div class="disc" class:spinning={player.playing}>
		<div class="grooves"></div>
		<div class="label">
			{#key cover}
				{#if cover}
					<img src={cover} alt="" class="fade" use:coverFallback />
				{:else}
					<div class="placeholder fade"></div>
				{/if}
			{/key}
		</div>
		<div class="shine"></div>
	</div>
	<div class="arm" class:down={player.playing}>
		<span class="pivot"></span>
		<span class="rod"></span>
		<span class="head"></span>
	</div>
</div>

<style>
	.turntable {
		position: relative;
		width: var(--size);
		height: var(--size);
		flex: none;
	}
	.disc {
		position: absolute;
		inset: 0;
		border-radius: 50%;
		background: radial-gradient(circle, #2b2a29 0 30%, #161616 31%, #1f1e1d 60%, #121212 100%);
		box-shadow:
			0 30px 60px -25px var(--shadow),
			0 0 0 6px color-mix(in oklch, var(--surface) 60%, transparent);
		animation: spin 22s linear infinite;
		animation-play-state: paused;
	}
	.disc.spinning {
		animation-play-state: running;
	}
	.grooves {
		position: absolute;
		inset: 4%;
		border-radius: 50%;
		background: repeating-radial-gradient(circle, rgb(255 255 255 / 0.035) 0 1px, transparent 1px 4px);
	}
	.label {
		position: absolute;
		inset: 23%;
		border-radius: 50%;
		overflow: hidden;
		box-shadow: 0 0 0 3px #0e0e0e;
		background: var(--accent-soft);
	}
	.label img,
	.placeholder {
		width: 100%;
		height: 100%;
		object-fit: cover;
	}
	.placeholder {
		background: conic-gradient(from 30deg, var(--accent), var(--accent-2), var(--accent));
	}
	.label::after {
		/* spindle hole */
		content: '';
		position: absolute;
		left: 50%;
		top: 50%;
		width: 7%;
		height: 7%;
		transform: translate(-50%, -50%);
		border-radius: 50%;
		background: var(--bg);
		box-shadow: inset 0 1px 2px rgb(0 0 0 / 0.5);
	}
	.shine {
		position: absolute;
		inset: 0;
		border-radius: 50%;
		background: conic-gradient(
			from 200deg,
			transparent 0 10%,
			rgb(255 255 255 / 0.08) 15%,
			transparent 22% 60%,
			rgb(255 255 255 / 0.06) 66%,
			transparent 72%
		);
		/* the reflection does not rotate with the disc */
		animation: spin 22s linear infinite reverse;
		animation-play-state: inherit;
	}
	.fade {
		animation: fadein 900ms var(--ease-soft);
	}
	.arm {
		position: absolute;
		top: -4%;
		right: -2%;
		width: 34%;
		height: 62%;
		transform-origin: 78% 10%;
		transform: rotate(-26deg);
		transition: transform 600ms var(--ease-soft);
		pointer-events: none;
	}
	.arm.down {
		transform: rotate(0deg);
	}
	.pivot {
		position: absolute;
		right: 12%;
		top: 2%;
		width: 26%;
		aspect-ratio: 1;
		border-radius: 50%;
		background: radial-gradient(circle at 35% 35%, var(--surface), color-mix(in oklch, var(--text) 30%, var(--surface)));
		box-shadow: 0 4px 10px -4px var(--shadow);
	}
	.rod {
		position: absolute;
		right: 23%;
		top: 10%;
		width: 3.5%;
		height: 78%;
		border-radius: 3px;
		background: linear-gradient(90deg, color-mix(in oklch, var(--text) 25%, var(--surface)), var(--surface));
		transform: rotate(14deg);
		transform-origin: top center;
	}
	.head {
		position: absolute;
		left: 38%;
		bottom: 4%;
		width: 16%;
		height: 14%;
		border-radius: 3px;
		background: color-mix(in oklch, var(--text) 55%, var(--surface));
		transform: rotate(14deg);
	}
	@keyframes spin {
		to {
			transform: rotate(360deg);
		}
	}
	@keyframes fadein {
		from {
			opacity: 0;
		}
	}
</style>
