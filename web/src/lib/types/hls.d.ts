// Minimal typings for the parts of hls.js we use. hls.js ships its own
// types; when it is installed TypeScript resolves those first and this
// file is simply unused.
declare module 'hls.js' {
	export interface HlsErrorData {
		fatal?: boolean;
		type?: string;
		details?: string;
	}
	export default class Hls {
		static isSupported(): boolean;
		static readonly Events: { readonly ERROR: 'hlsError'; readonly MANIFEST_PARSED: 'hlsManifestParsed' };
		constructor(config?: Record<string, unknown>);
		loadSource(url: string): void;
		attachMedia(media: HTMLMediaElement): void;
		on(event: 'hlsError', listener: (event: 'hlsError', data: HlsErrorData) => void): void;
		destroy(): void;
	}
}

