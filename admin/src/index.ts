import "@fontsource-variable/geist/wght.css";
import "@fontsource-variable/spline-sans-mono/wght.css";
import "uno.css";
import "@admin/app.css";

import { mount } from "svelte";

import type { AdminPlugin, FieldPlugin } from "@riducms/plugin";
import type { RiduClient, RiduConfigShape } from "@riducms/sdk";
import type { TranslationLanguage } from "@riducms/translations";

import App from "@admin/app.svelte";
import type { AdminClient } from "@admin/core/api/admin-client";

export interface MountAdminOptions<Config extends RiduConfigShape> {
	/** Existing DOM element that receives the Ridu admin application. */
	target: Element;
	/** Creates the generated, application-specific Fetch client. */
	clientFactory: () => RiduClient<Config>;
	/** Route prefix used when the admin is not mounted at /admin. */
	adminBasePath?: string;
	/** Statically paired packaged plugins, including fields and routes. */
	plugins?: readonly AdminPlugin[];
	/** Application-local field renderers that have no packaged backend pair. */
	fieldPlugins?: readonly FieldPlugin[];
	/** Statically imported catalogs available to the Go-configured admin languages. */
	languages?: readonly TranslationLanguage[];
}

export function mountAdmin<Config extends RiduConfigShape>(
	options: MountAdminOptions<Config>
): ReturnType<typeof mount> {
	// AdminClient is the admin's capability-wide erased view. Generated clients keep exact
	// collection capabilities while sharing invariant runtime methods and envelopes; the manifest
	// gates which of those methods the admin exposes for each resource.
	const clientFactory: () => AdminClient = () => options.clientFactory() as unknown as AdminClient;
	return mount(App, {
		target: options.target,
		props: {
			clientFactory,
			...(options.adminBasePath === undefined ? {} : { adminBasePath: options.adminBasePath }),
			...(options.plugins === undefined ? {} : { plugins: options.plugins }),
			...(options.fieldPlugins === undefined ? {} : { fieldPlugins: options.fieldPlugins }),
			...(options.languages === undefined ? {} : { languages: options.languages }),
		},
	});
}

export type { AdminClient } from "@admin/core/api/admin-client";
