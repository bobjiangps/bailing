// Writes static/manifest.webmanifest for the edition being built.
//
//   VITE_BAILING_APP=all | photos | music   (default all)
//   BASE_PATH=/bailing                       (same as svelte.config.js)
//
// Run automatically before `npm run dev` / `npm run build` (see package.json).
import { copyFileSync, writeFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';

const app = ['photos', 'music'].includes(process.env.VITE_BAILING_APP) ? process.env.VITE_BAILING_APP : 'all';
const base = (process.env.BASE_PATH ?? '').replace(/\/$/, '');

const editions = {
	all: { name: '百灵 Bailing', short: '百灵', desc: '家庭相册与音乐', start: '/', bg: '#EFEBE6' },
	photos: { name: '百灵相册 Bailing Photos', short: '百灵相册', desc: '家庭相册', start: '/albums', bg: '#F3EEE3' },
	music: { name: '百灵音乐 Bailing Music', short: '百灵音乐', desc: '家庭音乐、电台与在线音乐', start: '/music', bg: '#EEF2F3' }
};
const e = editions[app];
const icon = (s) => ({ src: `${base}/icons/${app}-${s}.png`, sizes: `${s}x${s}`, type: 'image/png' });

const manifest = {
	id: `${base}/?app=${app}`,
	name: e.name,
	short_name: e.short,
	description: e.desc,
	lang: 'zh-CN',
	start_url: `${base}${e.start}`,
	scope: `${base}/`,
	display: 'standalone',
	background_color: e.bg,
	theme_color: e.bg,
	icons: [
		icon(192),
		icon(512),
		{ ...icon(512), purpose: 'maskable' },
		{ src: `${base}/icons/${app}.svg`, sizes: 'any', type: 'image/svg+xml' }
	]
};

const out = join(dirname(fileURLToPath(import.meta.url)), '..', 'static', 'manifest.webmanifest');
writeFileSync(out, JSON.stringify(manifest, null, 2) + '\n');
// iOS home-screen icon of this edition
const icons = join(dirname(out), 'icons');
copyFileSync(join(icons, `${app}-180.png`), join(icons, 'apple-touch-icon.png'));
console.log(`manifest: ${app} edition -> static/manifest.webmanifest`);



