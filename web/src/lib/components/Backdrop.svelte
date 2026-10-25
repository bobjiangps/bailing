<script lang="ts">
	// Slowly drifting colour blobs + paper grain. Only transforms are
	// animated (compositor-friendly); a gentle mouse parallax on desktop.
	let { tint = '' }: { tint?: string } = $props();

	let px = $state(0);
	let py = $state(0);

	function onMove(e: PointerEvent) {
		if (e.pointerType !== 'mouse') return;
		px = (e.clientX / innerWidth - 0.5) * 24;
		py = (e.clientY / innerHeight - 0.5) * 24;
	}
</script>

<svelte:window onpointermove={onMove} />

<div class="backdrop" aria-hidden="true" style:--tint={tint || 'transparent'}>
	<div class="parallax" style:transform="translate3d({px * 0.5}px, {py * 0.5}px, 0)">
		<span class="blob b1"></span>
		<span class="blob b2"></span>
		<span class="blob b3"></span>
	</div>
	<span class="tint"></span>
	<svg class="grain" xmlns="http://www.w3.org/2000/svg">
		<filter id="grain-f">
			<feTurbulence type="fractalNoise" baseFrequency="0.85" numOctaves="2" stitchTiles="stitch" />
			<feColorMatrix type="saturate" values="0" />
		</filter>
		<rect width="100%" height="100%" filter="url(#grain-f)" />
	</svg>
</div>

<style>
	.backdrop {
		position: fixed;
		inset: 0;
		z-index: 0;
		overflow: hidden;
		pointer-events: none;
	}
	.parallax {
		position: absolute;
		inset: -10%;
		transition: transform 1.2s var(--ease-soft);
	}
	.blob {
		position: absolute;
		width: 52vmax;
		height: 52vmax;
		border-radius: 50%;
		filter: blur(70px);
		opacity: 0.85;
		will-change: transform;
		transition: background-color 1.2s var(--ease-soft);
	}
	.b1 {
		background: var(--bg-blob-1);
		top: -12%;
		left: -8%;
		animation: drift1 48s ease-in-out infinite alternate;
	}
	.b2 {
		background: var(--bg-blob-2);
		bottom: -18%;
		right: -10%;
		animation: drift2 56s ease-in-out infinite alternate;
	}
	.b3 {
		background: var(--bg-blob-3);
		top: 30%;
		left: 35%;
		width: 38vmax;
		height: 38vmax;
		animation: drift3 42s ease-in-out infinite alternate;
	}
	.tint {
		position: absolute;
		inset: 0;
		background: radial-gradient(60% 60% at 30% 35%, var(--tint), transparent 70%);
		opacity: 0.35;
		transition: background 1.2s var(--ease-soft);
	}
	.grain {
		position: absolute;
		inset: 0;
		width: 100%;
		height: 100%;
		opacity: 0.05;
		mix-blend-mode: multiply;
	}
	:global([data-mode='dark']) .grain {
		mix-blend-mode: screen;
		opacity: 0.04;
	}
	@keyframes drift1 {
		to {
			transform: translate3d(12vw, 8vh, 0) scale(1.08);
		}
	}
	@keyframes drift2 {
		to {
			transform: translate3d(-10vw, -6vh, 0) scale(0.94);
		}
	}
	@keyframes drift3 {
		to {
			transform: translate3d(-8vw, 10vh, 0) scale(1.12);
		}
	}
	@media (max-width: 640px) {
		.blob {
			filter: blur(50px);
		}
	}
</style>
