import type { Component } from "svelte";

export { default as RichText } from "#richtext/rich-text.svelte";
export type RichTextBlockComponents<Payload extends { blockType: string }> = {
	[Slug in Payload["blockType"]]: Component<{ block: Extract<Payload, { blockType: Slug }> }>;
};
