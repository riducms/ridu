import overlayFile from './overlays.json';
import type { ReferenceParameter, ReferenceReturn } from '../types';

export interface ReferenceOverlayParameter extends Omit<ReferenceParameter, 'href'> {
	targetID?: string;
}

export interface ReferenceEditorialOverlay {
	summary: string;
	group: string;
	parametersLabel: string;
	parameters: ReferenceOverlayParameter[];
	returns?: ReferenceReturn;
	details: string[];
	example: string;
	keywords: string[];
	relatedDocs: string[];
	relatedSymbols: string[];
	typeLinkIDs: Record<string, string>;
}

interface ReferenceOverlayFile {
	schemaVersion: 1;
	overlays: Record<string, ReferenceEditorialOverlay>;
}

const catalog = overlayFile as ReferenceOverlayFile;
if (catalog.schemaVersion !== 1) throw new Error('Unsupported reference overlay version');

/** Reviewed prose keyed only by source-stable declaration identity. */
export const referenceEditorialOverlays: Readonly<Record<string, ReferenceEditorialOverlay>> =
	catalog.overlays;
