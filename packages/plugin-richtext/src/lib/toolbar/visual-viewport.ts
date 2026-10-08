/**
 * Keeps a sticky element in the visible part of the page. With an on-screen keyboard open, a phone
 * can scroll the visible area below the top of the layout viewport, where `position: sticky`
 * measures from; the element's `top` reads this offset from `--_ridu-richtext-viewport-offset`.
 */
export function followVisualViewport(element: HTMLElement) {
	const viewport = window.visualViewport;
	if (viewport === null) return;
	const follow = () =>
		element.style.setProperty("--_ridu-richtext-viewport-offset", `${viewport.offsetTop}px`);
	follow();
	viewport.addEventListener("scroll", follow);
	viewport.addEventListener("resize", follow);
	return () => {
		viewport.removeEventListener("scroll", follow);
		viewport.removeEventListener("resize", follow);
	};
}
