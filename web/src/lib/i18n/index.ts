import { prefs } from '$lib/stores/prefs.svelte';
import zh from './zh';
import en from './en';

const dicts = { zh, en };

/**
 * Translate a key. Because it reads `prefs.lang` (a $state), any template that
 * calls t() re-renders automatically when the language changes.
 */
export function t(key: string, params?: Record<string, string | number>): string {
	let s = dicts[prefs.lang][key] ?? zh[key];
	if (typeof s !== 'string') return key;
	if (params) s = s.replace(/\{(\w+)\}/g, (_, k: string) => String(params[k] ?? ''));
	return s;
}

/** Translate a key whose value is a list of strings. */
export function tl(key: string): string[] {
	const v = dicts[prefs.lang][key] ?? zh[key];
	return Array.isArray(v) ? v : [];
}
