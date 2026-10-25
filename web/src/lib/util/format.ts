/** 1024-based byte formatting: 1536 -> "1.5 KB", 1073741824 -> "1.00 GB". */
export function formatBytes(n: number): string {
	if (!n || n < 0) return '0 B';
	const units = ['B', 'KB', 'MB', 'GB', 'TB'];
	let i = 0;
	let v = n;
	while (v >= 1024 && i < units.length - 1) {
		v /= 1024;
		i++;
	}
	const digits = i === 0 ? 0 : v < 10 ? 2 : v < 100 ? 1 : 0;
	return `${v.toFixed(digits)} ${units[i]}`;
}

/** Seconds -> "m:ss" or "h:mm:ss". */
export function formatTime(sec: number): string {
	if (!isFinite(sec) || sec < 0) sec = 0;
	const s = Math.floor(sec % 60);
	const m = Math.floor(sec / 60) % 60;
	const h = Math.floor(sec / 3600);
	const ss = String(s).padStart(2, '0');
	return h > 0 ? `${h}:${String(m).padStart(2, '0')}:${ss}` : `${m}:${ss}`;
}

/** Random id for traffic stream sessions. */
export function randomId(len = 16): string {
	const a = new Uint8Array(len);
	crypto.getRandomValues(a);
	return Array.from(a, (b) => 'abcdefghijklmnopqrstuvwxyz0123456789'[b % 36]).join('');
}

export function clamp(v: number, lo: number, hi: number) {
	return Math.min(hi, Math.max(lo, v));
}

export const prefersReducedMotion = () =>
	typeof matchMedia !== 'undefined' && matchMedia('(prefers-reduced-motion: reduce)').matches;
