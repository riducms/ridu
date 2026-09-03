import { definePreset } from 'unocss';

export const presetSiteSearch = definePreset(() => ({
	name: 'ridu-site-search',
	shortcuts: {
		'search-trigger':
			'grid shrink-0 cursor-pointer items-center rd-2px border border-line bg-transparent text-left text-ink-faint transition-colors duration-140 hover:(border-line-strong bg-surface text-ink-soft) aria-[expanded=true]:(border-line-strong bg-surface text-ink-soft) lt-md:(ml-1.4 size-10) md:h-9.25 md:lt-xl:(ml-auto w-9.5) lt-xl:(justify-items-center px-0) xl:(ml-9.6 w-[min(420px,32vw)] grid-cols-[auto_minmax(0,1fr)_auto] justify-items-start gap-2.6 px-[0.6rem] pl-[0.8rem])',
		'search-trigger-icon':
			'size-1em fill-none stroke-current stroke-width-[1.7] [stroke-linecap:round] [stroke-linejoin:round]',
		'search-trigger-label': 'hidden text-14px xl:inline',
		'search-trigger-shortcut':
			'hidden px-[0.36rem] py-[0.06rem] font-mono text-10px text-ink-faint xl:(block justify-self-end)',
		'search-keycap':
			'border border-line-strong rd-2px bg-surface-raised/72 shadow-[inset_0_-1px_0_rgba(255,255,255,0.04)]',
		'search-dialog':
			'fixed top-[clamp(4.25rem,10vh,7rem)] right-auto bottom-auto left-1/2 m-0 max-h-[min(720px,calc(100dvh-clamp(5.25rem,12vh,8rem)))] w-[min(760px,calc(100%-2rem))] max-w-none -translate-x-1/2 overflow-hidden border border-line-strong rd-4px bg-surface p-0 text-ink lt-sm:(top-3 max-h-[calc(100dvh-1.5rem)] w-[calc(100%-1rem)])',
		'search-shell': 'grid max-h-[inherit] min-w-0 w-full grid-rows-[auto_auto_minmax(0,1fr)_auto]',
		'search-input-row':
			'flex min-h-16.6 min-w-0 items-center gap-3.4 border-b border-line px-4 lt-sm:(min-h-15 gap-2.8 px-3)',
		'search-input-icon':
			'size-4.8 flex-none fill-none stroke-current stroke-width-[1.65] text-ink-soft [stroke-linecap:round] [stroke-linejoin:round]',
		'search-input':
			'min-w-0 flex-1 border-0 bg-transparent text-16px text-ink outline-0 placeholder:text-ink-faint lt-sm:text-[0.92rem]',
		'search-close':
			'flex-none cursor-pointer border-0 bg-transparent p-1.2 text-ink-faint hover:text-ink',
		'search-close-keycap': 'block px-[0.36rem] py-[0.14rem] font-mono text-[0.62rem] leading-[1.2]',
		'search-filter-row':
			'flex min-h-11.6 min-w-0 items-center justify-between gap-4 border-b border-line px-3 lt-sm:(overflow-x-auto px-1.8)',
		'search-filters': 'flex min-w-0 items-center gap-0.6',
		'search-filter':
			'inline-flex h-7.4 cursor-pointer items-center gap-[0.38rem] border border-transparent rd-3px bg-transparent px-2.2 font-mono text-[0.66rem] font-[560] tracking-[0.06em] text-ink-faint uppercase transition-colors duration-120 hover:text-ink-soft lt-sm:(px-[0.43rem] text-[0.59rem])',
		'search-filter-count': 'text-[0.58rem] text-ink-faint tabular-nums',
		'search-filter-hint': 'flex-none font-mono text-[0.61rem] text-ink-faint lt-sm:hidden',
		'search-control-keycap': 'px-[0.27rem] py-[0.08rem] font-inherit',
		'search-results-viewport':
			'h-[clamp(0px,calc(100dvh-15rem),30rem)] min-h-0 min-w-0 overflow-y-auto overscroll-contain p-1.8 lt-sm:(h-[clamp(0px,calc(100dvh-10.9rem),30rem)] p-1.4)',
		'search-state':
			'flex min-h-64 flex-col items-center justify-center px-6 py-10 text-center lt-sm:min-h-52',
		'search-state-icon':
			'mb-3.6 size-5.6 fill-none stroke-current stroke-width-[1.65] text-ink-faint [stroke-linecap:round] [stroke-linejoin:round]',
		'search-state-title': 'm-0 text-[0.9rem] font-[620] text-ink-soft',
		'search-state-detail': 'mt-[0.38rem] max-w-104 text-12px leading-[1.55] text-ink-faint',
		'search-results': 'grid gap-[0.18rem]',
		'search-summary':
			'overflow-hidden text-ellipsis whitespace-nowrap font-mono text-[0.61rem] text-ink-faint lt-sm:hidden',
		'search-all': 'flex-none font-mono text-[0.66rem] text-accent no-underline hover:underline',
		'search-keyboard':
			'flex flex-none items-center gap-3.2 font-mono text-[0.61rem] text-ink-faint lt-sm:gap-2',
		'search-keyboard-hint': 'inline-flex items-center gap-[0.24rem]',
		'search-keyboard-primary': 'lt-sm:hidden',
		'search-footer':
			'flex min-h-10.6 min-w-0 items-center justify-between gap-4 border-t border-line px-3.4 lt-sm:(min-h-9.8 px-2.8)'
	}
}));
