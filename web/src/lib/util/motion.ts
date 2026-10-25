/**
 * Physically based spring easing without a library.
 *
 * We simulate a damped spring once (x'' = -k(x-1) - c x') and sample it into a
 * CSS `linear()` easing function, which the browser then runs on the
 * compositor via the Web Animations API (element.animate).
 * Browsers without linear() support get a soft cubic-bezier instead.
 */
const cache = new Map<string, { easing: string; duration: number }>();

export function spring(stiffness = 120, damping = 20, mass = 1) {
	const key = `${stiffness}/${damping}/${mass}`;
	const hit = cache.get(key);
	if (hit) return hit;

	const dt = 1 / 120;
	let x = 0;
	let v = 0;
	let t = 0;
	const samples: number[] = [0];
	// run until settled (or 3s max)
	while (t < 3) {
		const a = (-stiffness * (x - 1) - damping * v) / mass;
		v += a * dt;
		x += v * dt;
		t += dt;
		samples.push(x);
		if (Math.abs(x - 1) < 0.001 && Math.abs(v) < 0.01) break;
	}
	samples[samples.length - 1] = 1;
	// keep ~60 points to keep the string small
	const step = Math.max(1, Math.floor(samples.length / 60));
	const pts: string[] = [];
	for (let i = 0; i < samples.length; i += step) pts.push(samples[i].toFixed(4));
	if (pts[pts.length - 1] !== '1.0000') pts.push('1');

	const supported =
		typeof CSS !== 'undefined' && CSS.supports?.('animation-timing-function', 'linear(0, 1)');
	const out = {
		easing: supported ? `linear(${pts.join(', ')})` : 'cubic-bezier(.22,.8,.24,1)',
		duration: Math.round(t * 1000)
	};
	cache.set(key, out);
	return out;
}

/** Animate a transform with a spring; resolves when finished. */
export function springTransform(
	el: Element,
	from: string,
	to: string,
	opts: { stiffness?: number; damping?: number; fill?: FillMode } = {}
): Promise<void> {
	const s = spring(opts.stiffness ?? 120, opts.damping ?? 20);
	const anim = el.animate([{ transform: from }, { transform: to }], {
		duration: s.duration,
		easing: s.easing,
		fill: opts.fill ?? 'none'
	});
	return anim.finished.then(
		() => undefined,
		() => undefined
	);
}

/** Tiny rAF tween for numeric state (used by the photo viewer). */
export function tween(
	from: number[],
	to: number[],
	ms: number,
	onFrame: (v: number[]) => void
): () => void {
	const start = performance.now();
	let raf = 0;
	const ease = (p: number) => 1 - Math.pow(1 - p, 3);
	const frame = (now: number) => {
		const p = Math.min(1, (now - start) / ms);
		const e = ease(p);
		onFrame(from.map((f, i) => f + (to[i] - f) * e));
		if (p < 1) raf = requestAnimationFrame(frame);
	};
	raf = requestAnimationFrame(frame);
	return () => cancelAnimationFrame(raf);
}
