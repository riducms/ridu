export type CopyFeedbackResult = Readonly<{
	source: string;
	copied: boolean;
}>;

/** Own short-lived clipboard feedback for a component instance. */
export class CopyFeedback {
	result = $state.raw<CopyFeedbackResult>();

	#active = true;
	#revision = 0;
	#timer: number | undefined;

	constructor(private readonly duration = 1_500) {
		$effect(() => () => {
			this.#active = false;
			this.#revision += 1;
			if (this.#timer !== undefined) window.clearTimeout(this.#timer);
		});
	}

	async copy(source: string) {
		const revision = ++this.#revision;
		if (this.#timer !== undefined) window.clearTimeout(this.#timer);
		this.#timer = undefined;
		this.result = undefined;

		let copied = true;
		try {
			await navigator.clipboard.writeText(source);
		} catch {
			copied = false;
		}

		if (!this.#active || revision !== this.#revision) return;
		this.result = { source, copied };
		this.#timer = window.setTimeout(() => {
			if (!this.#active || revision !== this.#revision) return;
			this.result = undefined;
			this.#timer = undefined;
		}, this.duration);
	}
}
