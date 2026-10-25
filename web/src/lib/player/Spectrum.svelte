<script lang="ts">
	// Spectrum bars drawn from the Web Audio analyser (see fx.svelte.ts).
	import { onDestroy } from 'svelte';
	import { prefs } from '$lib/stores/prefs.svelte';
	import { fx } from './fx.svelte';
	import { player } from './player.svelte';

	let { width = 320, height = 56, bars = 48 }: { width?: number; height?: number; bars?: number } = $props();

	let canvas: HTMLCanvasElement | undefined = $state();
	let raf = 0;
	let data: Uint8Array<ArrayBuffer> | null = null;
	const levels = new Float32Array(128);

	function draw() {
		raf = requestAnimationFrame(draw);
		const c = canvas;
		if (!c) return;
		const ctx = c.getContext('2d');
		if (!ctx) return;
		const dpr = devicePixelRatio || 1;
		if (c.width !== width * dpr) {
			c.width = width * dpr;
			c.height = height * dpr;
		}
		ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
		ctx.clearRect(0, 0, width, height);
		if (!data || data.length !== fx.bins) data = new Uint8Array(fx.bins);
		const have = player.playing && fx.spectrum(data);
		const color = getComputedStyle(c).color;
		ctx.fillStyle = color;
		const nyquist = fx.sampleRate / 2;
		const gap = 2;
		const bw = (width - gap * (bars - 1)) / bars;
		for (let i = 0; i < bars; i++) {
			// log-spaced bands between 40 Hz and 16 kHz
			const f0 = 40 * Math.pow(16000 / 40, i / bars);
			const f1 = 40 * Math.pow(16000 / 40, (i + 1) / bars);
			let v = 0;
			if (have && data) {
				const a = Math.floor((f0 / nyquist) * data.length);
				const b = Math.max(a + 1, Math.floor((f1 / nyquist) * data.length));
				for (let k = a; k < b && k < data.length; k++) v = Math.max(v, data[k]);
				v /= 255;
			}
			// fall slowly, rise fast
			levels[i] = v > levels[i] ? v : levels[i] * 0.9;
			const h = Math.max(2, levels[i] * height);
			ctx.globalAlpha = 0.35 + 0.65 * levels[i];
			ctx.beginPath();
			ctx.roundRect(i * (bw + gap), height - h, bw, h, Math.min(2, bw / 2));
			ctx.fill();
		}
	}

	$effect(() => {
		if (prefs.spectrum && fx.attached && canvas) {
			cancelAnimationFrame(raf);
			raf = requestAnimationFrame(draw);
		} else cancelAnimationFrame(raf);
		return () => cancelAnimationFrame(raf);
	});

	onDestroy(() => cancelAnimationFrame(raf));
</script>

{#if prefs.spectrum && fx.attached}
	<canvas bind:this={canvas} style:width="{width}px" style:height="{height}px" aria-hidden="true"></canvas>
{/if}

<style>
	canvas {
		display: block;
		color: var(--accent);
	}
</style>

