// Minimal typings for page-flip (StPageFlip, MIT). The npm package ships
// only the browser bundle without .d.ts files, so we declare the parts we use.
declare module 'page-flip' {
	export interface FlipSetting {
		width: number;
		height: number;
		size?: 'fixed' | 'stretch';
		minWidth?: number;
		maxWidth?: number;
		minHeight?: number;
		maxHeight?: number;
		startPage?: number;
		drawShadow?: boolean;
		flippingTime?: number;
		usePortrait?: boolean;
		startZIndex?: number;
		autoSize?: boolean;
		maxShadowOpacity?: number;
		showCover?: boolean;
		mobileScrollSupport?: boolean;
		clickEventForward?: boolean;
		useMouseEvents?: boolean;
		swipeDistance?: number;
		showPageCorners?: boolean;
		disableFlipByClick?: boolean;
	}

	export interface WidgetEvent<T = unknown> {
		data: T;
		object: PageFlip;
	}

	export class PageFlip {
		constructor(element: HTMLElement, setting: Partial<FlipSetting>);
		loadFromHTML(items: NodeListOf<HTMLElement> | HTMLElement[]): void;
		loadFromImages(images: string[]): void;
		updateFromHtml(items: NodeListOf<HTMLElement> | HTMLElement[]): void;
		destroy(): void;
		update(): void;
		turnToPage(page: number): void;
		turnToNextPage(): void;
		turnToPrevPage(): void;
		flip(page: number, corner?: 'top' | 'bottom'): void;
		flipNext(corner?: 'top' | 'bottom'): void;
		flipPrev(corner?: 'top' | 'bottom'): void;
		getPageCount(): number;
		getCurrentPageIndex(): number;
		getOrientation(): 'portrait' | 'landscape';
		on(event: string, callback: (e: WidgetEvent<any>) => void): PageFlip; // eslint-disable-line @typescript-eslint/no-explicit-any
		off(event: string): void;
	}
}

