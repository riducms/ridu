import type { UploadImageEdit } from "@riducms/protocol";
export type { UploadImageEdit } from "@riducms/protocol";

export const uploadImageEditFields = [
	"focalX",
	"focalY",
	"cropX",
	"cropY",
	"cropWidth",
	"cropHeight",
] as const satisfies readonly (keyof UploadImageEdit)[];

export const uploadMetadataFields = [
	"filename",
	"mimeType",
	"filesize",
	"url",
	"objectKey",
	"width",
	"height",
	"sizes",
	"source",
	...uploadImageEditFields,
] as const;

const uploadMetadataFieldSet = new Set<string>(uploadMetadataFields);

export function isUploadMetadataField(name: string) {
	return uploadMetadataFieldSet.has(name);
}
