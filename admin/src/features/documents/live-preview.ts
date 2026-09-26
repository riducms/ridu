import type { SchemaLivePreview } from "@riducms/protocol";
import { RIDU_LIVE_PREVIEW_CHANNEL_PARAM, RIDU_PREVIEW_TOKEN_PARAM } from "@riducms/sdk";

import type { FormValues } from "@admin/core/forms/form-schema";
import { readDocumentPath } from "@admin/core/schema/read-document-path";

export interface LivePreviewContext {
	collection: string;
	documentID: string;
	values: FormValues;
}

const placeholder = /\{(id|collection|field:([^{}]+))\}/g;

export function resolveLivePreviewURL(
	preview: SchemaLivePreview,
	context: LivePreviewContext,
	baseURL: string
) {
	const path = preview.url.replace(placeholder, (_match, token: string, fieldPath?: string) => {
		const value =
			token === "id"
				? context.documentID
				: token === "collection"
					? context.collection
					: readDocumentPath(context.values, fieldPath ?? "");
		return encodeURIComponent(stringValue(value));
	});
	return new URL(path, baseURL).toString();
}

export function livePreviewTargetOrigin(url: string) {
	return new URL(url).origin;
}

export function addLivePreviewChannel(url: string, channel: string) {
	const target = new URL(url);
	target.searchParams.set(RIDU_LIVE_PREVIEW_CHANNEL_PARAM, channel);
	return target.toString();
}

export function addLivePreviewToken(url: string, token: string) {
	const target = new URL(url);
	target.searchParams.set(RIDU_PREVIEW_TOKEN_PARAM, token);
	return target.toString();
}

function stringValue(value: unknown) {
	if (typeof value === "string" || typeof value === "number" || typeof value === "boolean") {
		return String(value);
	}
	return "";
}
