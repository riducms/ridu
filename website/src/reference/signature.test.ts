/// <reference types="bun-types" />

import { describe, expect, test } from 'bun:test';
import { referenceModules } from './generated';
import { formatReferenceSignature, tokenizeReferenceSignature } from './signature';

describe('formatReferenceSignature', () => {
	test('keeps short declarations on one line', () => {
		expect(formatReferenceSignature('func Allow() AccessDecision')).toBe(
			'func Allow() AccessDecision'
		);
	});

	test('wraps top-level parameters without splitting nested generics', () => {
		expect(
			formatReferenceSignature(
				'function createClient<Config extends Shape>(options: ClientOptions, middleware: Middleware<Map<string, number>>): Client<Config>'
			)
		).toBe(
			'function createClient<Config extends Shape>(\n\toptions: ClientOptions,\n\tmiddleware: Middleware<Map<string, number>>,\n): Client<Config>'
		);
	});

	test('skips a Go receiver before wrapping method parameters', () => {
		expect(
			formatReferenceSignature('func (api *LocalAPI) Find(ctx context.Context, id string) error')
		).toBe('func (api *LocalAPI) Find(\n\tctx context.Context,\n\tid string,\n) error');
	});

	test('formats declaration members at top level', () => {
		expect(formatReferenceSignature('interface Result { value: string; close(): void }')).toBe(
			'interface Result {\n\tvalue: string\n\tclose(): void\n}'
		);
	});

	test('formats grouped Go declarations as one declaration per line', () => {
		expect(
			formatReferenceSignature(
				'const ( FieldCheckbox FieldType = "checkbox"; FieldCountry FieldType = "country"; FieldDate FieldType = "date" )'
			)
		).toBe(
			'const (\n\tFieldCheckbox FieldType = "checkbox"\n\tFieldCountry FieldType = "country"\n\tFieldDate FieldType = "date"\n)'
		);
		expect(
			formatReferenceSignature(
				'const RIDU_LIVE_PREVIEW_MESSAGE = "ridu-live-preview"; const RIDU_LIVE_PREVIEW_CHANNEL_PARAM = "__ridu_preview"; const RIDU_PREVIEW_TOKEN_PARAM = "__ridu_preview_token"'
			)
		).toBe(
			'const RIDU_LIVE_PREVIEW_MESSAGE = "ridu-live-preview";\nconst RIDU_LIVE_PREVIEW_CHANNEL_PARAM = "__ridu_preview";\nconst RIDU_PREVIEW_TOKEN_PARAM = "__ridu_preview_token"'
		);
		expect(
			formatReferenceSignature(
				'var ErrNotFound, ErrConflict, ErrAuthInitialized, ErrDeleteRestricted, ErrPopulationLimit, ErrTaskLeaseLost error'
			)
		).toContain('var (\n\tErrNotFound error\n\tErrConflict error');
		expect(
			formatReferenceSignature(
				'var PrimaryUpdates, SecondaryUpdates, ArchivedUpdates, ReplicatedDocumentUpdates chan<- DocumentUpdateEvent'
			)
		).toContain(
			'var (\n\tPrimaryUpdates chan<- DocumentUpdateEvent\n\tSecondaryUpdates chan<- DocumentUpdateEvent'
		);
	});

	test('formats long unions, commands, nested objects, and generic arguments', () => {
		expect(
			formatReferenceSignature(
				'type FieldType = "text" | "code" | "select" | "radio" | "relationship" | "upload" | "group" | "textarea" | "email" | "date" | "number" | "checkbox"'
			)
		).toContain('\n\t| "text"\n\t| "code"');
		expect(
			formatReferenceSignature(
				'type Confirmation = { message: string; redirectTo: string; showSummary: boolean } | { message: string; showSummary: false; resetForm: true }'
			)
		).toBe(
			'type Confirmation =\n\t| {\n\t\tmessage: string\n\t\tredirectTo: string\n\t\tshowSummary: boolean\n\t}\n\t| {\n\t\tmessage: string\n\t\tshowSummary: false\n\t\tresetForm: true\n\t}'
		);
		expect(
			formatReferenceSignature(
				'type ButtonProps = ComponentProps<{ appearance: "primary" | "secondary"; buttonStyle: "pill" | "rectangular"; disabled: boolean }>'
			)
		).not.toContain('\n\t\t\tappearance');
		expect(
			formatReferenceSignature(
				'ridu new [--template starter|blank] [--agent codex|claude|none] [--module <go-module>] [--scope <npm-scope>] [directory]'
			)
		).toBe(
			'ridu new\n\t[--template starter|blank]\n\t[--agent codex|claude|none]\n\t[--module <go-module>]\n\t[--scope <npm-scope>]\n\t[directory]'
		);
		expect(
			formatReferenceSignature(
				'--database-url <url> --allow-insecure-database --advisory-lock-wait <duration> --statement-timeout <duration>'
			)
		).toBe(
			'--database-url <url>\n--allow-insecure-database\n--advisory-lock-wait <duration>\n--statement-timeout <duration>'
		);
		expect(
			formatReferenceSignature(
				'interface Props { defaultView: Snippet; host: { login(credentials: { email: string; password: string }): Promise<void>; notify(tone: "success" | "error", title: string, message?: string): void } }'
			)
		).toContain(
			'host: {\n\t\tlogin(credentials: {\n\t\t\temail: string\n\t\t\tpassword: string\n\t\t}): Promise<void>'
		);
		expect(
			formatReferenceSignature(
				'count<Slug extends CollectionSlug<Config>>(collection: Slug, options?: Pick<ListOptions<WhereFor<Config, Slug>, never, never>, "where" | "trash" | "locale" | "fallbackLocale" | "signal" | "headers">): Promise<CountEnvelope>'
			)
		).toContain('options?: Pick<\n\t\tListOptions<WhereFor<Config, Slug>, never, never>,');
		expect(
			formatReferenceSignature(
				'parse(value: ExtremelyLongContainerName<ExtremelyLongSingleArgumentNameThatShouldRemainReadable>): ParsedReferenceValue'
			)
		).toContain(
			'value: ExtremelyLongContainerName<\n\t\tExtremelyLongSingleArgumentNameThatShouldRemainReadable\n\t>'
		);
	});

	test('keeps every catalog signature within the readable line budget', () => {
		const overlong = referenceModules.flatMap((module) =>
			module.symbols.flatMap((symbol) => {
				const longestLine = Math.max(
					...formatReferenceSignature(symbol.signature)
						.split('\n')
						.map((line) => line.length)
				);
				return longestLine > 100 ? [`${module.slug}/${symbol.slug}: ${longestLine}`] : [];
			})
		);
		expect(overlong).toEqual([]);
	});

	test('keeps every example within the readable line budget', () => {
		const renderedColumns = (line: string) => {
			let columns = 0;
			for (const character of line) {
				columns += character === '\t' ? 2 - (columns % 2) : 1;
			}
			return columns;
		};
		const overlong = referenceModules.flatMap((module) =>
			module.symbols.flatMap((symbol) =>
				symbol.example
					? symbol.example
							.split('\n')
							.flatMap((line, index) =>
								renderedColumns(line) > 72
									? [`${module.slug}/${symbol.slug}:${index + 1}: ${renderedColumns(line)}`]
									: []
							)
					: []
			)
		);
		expect(overlong).toEqual([]);
	});
});

describe('tokenizeReferenceSignature', () => {
	test('classifies symbols, parameters, linked types, and punctuation', () => {
		const tokens = tokenizeReferenceSignature('func New(config Config) (*App, error)', {
			name: 'New',
			parameters: [{ name: 'config' }],
			typeLinks: { Config: '/reference/ridu/config/', App: '/reference/core/app/' }
		});

		expect(tokens.find((token) => token.text === 'func')?.kind).toBe('keyword');
		expect(tokens.find((token) => token.text === 'New')?.kind).toBe('symbol');
		expect(tokens.find((token) => token.text === 'config')?.kind).toBe('parameter');
		expect(tokens.find((token) => token.text === 'Config')).toEqual({
			text: 'Config',
			kind: 'reference',
			href: '/reference/ridu/config/'
		});
		expect(tokens.find((token) => token.text === 'error')?.kind).toBe('type');
	});

	test('never treats inherited object properties as reference links', () => {
		const constructor = tokenizeReferenceSignature('ridu plugin add <key> --constructor <name>', {
			name: 'ridu plugin add',
			typeLinks: {}
		}).find((token) => token.text === 'constructor');

		expect(constructor?.kind).toBe('plain');
		expect(constructor?.href).toBeUndefined();
	});
});
