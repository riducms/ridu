import type { SchemaManifest } from "@riducms/protocol";
import type { AdminLoader } from "@riducms/sdk";
import type { Component } from "svelte";

import type { FieldDocument } from "#lib/authoring.js";
import type { AdminI18n } from "#lib/i18n.js";

/** Common data supplied to admin extension components. Use the generated SDK for requests. */
export interface AdminExtensionProps {
	/** Resolved Go schema available to this admin session; it may omit inaccessible resources. */
	manifest: SchemaManifest;
	/** Signed-in user document, absent on screens where no user is signed in yet. */
	user?: FieldDocument;
	/** Current admin interface translations and formatting preferences. */
	i18n: AdminI18n;
}

export type AdminExtensionNotificationTone = "success" | "error";

/** Prepared application data and the mounted view's explicit refresh operation. */
export interface AdminLoaderProps<Data> {
	data: Data;
	refresh: () => Promise<void>;
	refreshing: boolean;
}

export type AdminViewComponent<Props extends object> =
	| { component: Component<Props> | Component; loader?: never }
	| {
			component: Component<Props & AdminLoaderProps<unknown>>;
			loader: AdminLoader<never, unknown>;
	  };

/** Spread this pairing into a dashboard panel, custom route or core-view registration. */
export function withAdminLoader<Input, Data, Props extends AdminLoaderProps<NoInfer<Data>>>(
	loader: AdminLoader<Input, Data>,
	component: Component<Props>
): {
	loader: AdminLoader<never, unknown>;
	component: Component<Omit<Props, keyof AdminLoaderProps<unknown>> & AdminLoaderProps<unknown>>;
} {
	// Erase the paired generic only after checking both sides. The host decodes
	// with this same loader before handing its output to this same component.
	return { loader, component } as unknown as {
		loader: AdminLoader<never, unknown>;
		component: Component<Omit<Props, keyof AdminLoaderProps<unknown>> & AdminLoaderProps<unknown>>;
	};
}
