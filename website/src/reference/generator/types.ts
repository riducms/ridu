import type { ReferenceKind, ReferenceParameter, ReferenceSource } from '../types';

export interface ExtractedDeclaration {
	id: string;
	name: string;
	kind: ReferenceKind;
	signature: string;
	summary: string;
	source?: Omit<ReferenceSource, 'url'>;
	members: ReferenceParameter[];
	parameters: ReferenceParameter[];
	returns?: string;
	receiver?: string;
	aliases: string[];
	overloads: string[];
}

export interface GoExtractionCatalog {
	packages: Array<{
		importPath: string;
		name: string;
		declarations: Array<{
			id: string;
			name: string;
			kind: 'function' | 'method' | 'type' | 'interface' | 'const' | 'var';
			signature: string;
			summary: string;
			source: { path: string; line: number };
			members: ReferenceParameter[];
			parameters: ReferenceParameter[];
			returns?: string;
			receiver?: string;
			aliases: string[];
		}>;
	}>;
}
