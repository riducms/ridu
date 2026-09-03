import catalog from './catalog.json';
import type { ReferenceModule } from '../types';

interface GeneratedReferenceCatalog {
	schemaVersion: 1;
	modules: ReferenceModule[];
}

const generated = catalog as GeneratedReferenceCatalog;
if (generated.schemaVersion !== 1)
	throw new Error('Unsupported generated reference catalog version');

/** Committed deterministic output; website builds never inspect application source. */
export const referenceModules: ReferenceModule[] = generated.modules;
