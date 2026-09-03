import { createContext } from "svelte";
import { toast } from "svelte-sonner";

import ValidationToast from "@admin/core/notifications/validation-toast.svelte";

export type NotificationTone = "success" | "warning" | "error";

export interface NotificationInput {
	title: string;
	message?: string;
	duration?: number;
}

export interface ValidationNotificationInput {
	labels: readonly string[];
}

export class NotificationCenter {
	#ids = new Set<number | string>();

	success = (input: NotificationInput) => this.#show("success", input);

	warning = (input: NotificationInput) => this.#show("warning", input);

	error = (input: NotificationInput) => this.#show("error", input);

	validation = ({ labels }: ValidationNotificationInput) => {
		let id: number | string;
		const uniqueLabels = [...new Set(labels)];
		id = toast.error(ValidationToast, {
			componentProps: { labels: uniqueLabels },
			duration: 7_000,
			important: true,
			icon: null,
			onDismiss: () => this.#ids.delete(id),
			onAutoClose: () => this.#ids.delete(id),
		});
		this.#ids.add(id);
		return id;
	};

	dismiss = (id: number | string) => {
		toast.dismiss(id);
		this.#ids.delete(id);
	};

	destroy() {
		for (const id of this.#ids) toast.dismiss(id);
		this.#ids.clear();
	}

	#show(tone: NotificationTone, input: NotificationInput) {
		let id: number | string;
		const options = {
			description: input.message,
			duration: input.duration ?? (tone === "success" ? 4_500 : 7_000),
			important: tone === "error",
			onDismiss: () => this.#ids.delete(id),
			onAutoClose: () => this.#ids.delete(id),
		};
		id = toast[tone](input.title, options);
		this.#ids.add(id);
		return id;
	}
}

const [getNotificationCenter, setNotificationCenter] = createContext<NotificationCenter>();

export { getNotificationCenter, setNotificationCenter };
