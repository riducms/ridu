export type ReferenceKind =
	| 'function'
	| 'type'
	| 'method'
	| 'interface'
	| 'class'
	| 'constant'
	| 'variable'
	| 'component'
	| 'command';

export type ReferenceLanguage = 'go' | 'ts' | 'shell';

export interface ReferenceParameter {
	name: string;
	type: string;
	description: string;
	href?: string;
}

export interface ReferenceReturn {
	type: string;
	description: string;
}

export interface ReferenceSource {
	/** Repository-relative source path. Absolute build-machine paths are forbidden. */
	path: string;
	line: number;
	url: string;
}

export interface ReferenceSymbol {
	/** Source-stable identity. Routes and editorial overlays are keyed by this value. */
	id: string;
	name: string;
	slug: string;
	kind: ReferenceKind;
	group: string;
	summary: string;
	signature: string;
	parameters: ReferenceParameter[];
	parametersLabel: string;
	returns?: ReferenceReturn;
	details: string[];
	example: string;
	language: ReferenceLanguage;
	typeLinks: Record<string, string>;
	aliases: string[];
	source?: ReferenceSource;
	relatedDocs: string[];
	relatedSymbols: string[];
	overloads: string[];
}

export interface ReferenceModule {
	id: string;
	name: string;
	slug: string;
	packageName: string;
	language: ReferenceLanguage;
	group: string;
	summary: string;
	groupOrder: string[];
	symbols: ReferenceSymbol[];
}
