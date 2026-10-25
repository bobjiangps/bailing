/**
 * Audio effects: 10-band equaliser and spectrum analyser (Web Audio API).
 *
 * Browser rule that shapes this module: once an <audio> element is routed
 * through an AudioContext (createMediaElementSource), audio from another
 * origin is only audible if that origin allows CORS. Otherwise the browser
 * outputs silence. So:
 *  - the graph is created lazily, the first time the user turns the
 *    equaliser or the spectrum on, and cannot be detached again until the
 *    page reloads (a Web Audio limitation);
 *  - while it is attached, the player loads streams with crossOrigin set and
 *    sends sources without CORS through the server relay (see player).
 * Local tracks and relayed streams are same-origin and always work.
 */

export const EQ_BANDS = [31, 62, 125, 250, 500, 1000, 2000, 4000, 8000, 16000];

export const EQ_PRESETS: Record<string, number[]> = {
	flat: [0, 0, 0, 0, 0, 0, 0, 0, 0, 0],
	bass: [6, 5, 4, 2, 0, 0, 0, 0, 0, 0],
	vocal: [-2, -2, -1, 1, 3, 4, 3, 1, 0, -1],
	pop: [-1, 1, 3, 4, 3, 0, -1, -1, 1, 2],
	rock: [4, 3, 2, 0, -1, -1, 1, 3, 4, 4],
	jazz: [3, 2, 1, 2, -1, -1, 0, 1, 2, 3],
	classical: [4, 3, 2, 1, -1, -1, 0, 2, 3, 4],
	treble: [0, 0, 0, 0, 0, 1, 2, 4, 5, 6]
};

class AudioFx {
	/** graph attached to the <audio> element (stays true until reload) */
	attached = $state(false);
	/** AudioContext unsupported or failed */
	unavailable = $state(false);

	private ctx: AudioContext | null = null;
	private src: MediaElementAudioSourceNode | null = null;
	private filters: BiquadFilterNode[] = [];
	private analyser: AnalyserNode | null = null;
	private gains = [...EQ_PRESETS.flat];
	private eqOn = false;

	/** Attach the graph to the element. Returns false if not possible. */
	attach(el: HTMLAudioElement): boolean {
		if (this.attached) return true;
		const AC = window.AudioContext || (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext;
		if (!AC) {
			this.unavailable = true;
			return false;
		}
		try {
			const ctx = new AC();
			const src = ctx.createMediaElementSource(el);
			const filters = EQ_BANDS.map((f, i) => {
				const b = ctx.createBiquadFilter();
				b.type = i === 0 ? 'lowshelf' : i === EQ_BANDS.length - 1 ? 'highshelf' : 'peaking';
				b.frequency.value = f;
				b.Q.value = 1.1;
				b.gain.value = 0;
				return b;
			});
			const analyser = ctx.createAnalyser();
			analyser.fftSize = 2048;
			analyser.smoothingTimeConstant = 0.78;
			let node: AudioNode = src;
			for (const f of filters) {
				node.connect(f);
				node = f;
			}
			node.connect(analyser);
			analyser.connect(ctx.destination);
			this.ctx = ctx;
			this.src = src;
			this.filters = filters;
			this.analyser = analyser;
			this.attached = true;
			this.apply();
			// contexts start suspended until a user gesture
			el.addEventListener('play', () => this.ctx?.resume().catch(() => {}));
			ctx.resume().catch(() => {});
			return true;
		} catch {
			this.unavailable = true;
			return false;
		}
	}

	setEq(on: boolean, gains: number[]) {
		this.eqOn = on;
		this.gains = gains.slice(0, EQ_BANDS.length);
		this.apply();
	}

	private apply() {
		this.filters.forEach((f, i) => {
			const g = this.eqOn ? (this.gains[i] ?? 0) : 0;
			if (this.ctx) f.gain.setTargetAtTime(g, this.ctx.currentTime, 0.05);
			else f.gain.value = g;
		});
	}

	/** Fill `out` with spectrum magnitudes (0..255); false when no data. */
	spectrum(out: Uint8Array<ArrayBuffer>): boolean {
		if (!this.analyser) return false;
		this.analyser.getByteFrequencyData(out);
		return true;
	}

	get bins() {
		return this.analyser?.frequencyBinCount ?? 1024;
	}

	get sampleRate() {
		return this.ctx?.sampleRate ?? 44100;
	}
}

export const fx = new AudioFx();

