import { describe, expect, test } from 'bun:test';
import path from 'node:path';
import ts from 'typescript';
import { referenceEditorialOverlays } from './authoring/overlays';
import { findReferenceSymbol } from './index';

const repositoryRoot = path.resolve(import.meta.dir, '../../..');
const helpers = [
	{
		name: 'defineAdmin',
		id: 'ts:@riducms/plugin/admin#defineAdmin',
		file: 'packages/plugin/src/admin.ts',
		slug: 'define-admin'
	},
	{
		name: 'defineAdminPlugin',
		id: 'ts:@riducms/plugin/authoring/v1#defineAdminPlugin',
		file: 'packages/plugin/src/authoring/v1.ts',
		slug: 'define-admin-plugin'
	},
	{
		name: 'definePluginField',
		id: 'ts:@riducms/plugin/authoring/v1#definePluginField',
		file: 'packages/plugin/src/field.ts',
		slug: 'define-plugin-field'
	},
	{
		name: 'defineFieldComponent',
		id: 'ts:@riducms/plugin/authoring/v1#defineFieldComponent',
		file: 'packages/plugin/src/field.ts',
		slug: 'define-field-component'
	},
	{
		name: 'defineFieldEditor',
		id: 'ts:@riducms/plugin/editor#defineFieldEditor',
		file: 'packages/plugin/src/editor/registry.ts',
		slug: 'define-field-editor'
	},
	{
		name: 'defineRowLabel',
		id: 'ts:@riducms/plugin/admin#defineRowLabel',
		file: 'packages/plugin/src/local-row-label.ts',
		slug: 'define-row-label'
	}
];

describe('admin component API documentation', () => {
	test('explains arguments, options, results, and connections for each registration helper', () => {
		for (const helper of helpers) {
			const symbol = findReferenceSymbol('plugin', helper.slug);
			expect(symbol, helper.name).toBeDefined();
			if (!symbol) continue;
			expect(symbol.optionsLabel?.length, helper.name).toBeGreaterThan(0);
			expect(symbol.options?.length, helper.name).toBeGreaterThan(0);
			for (const parameter of symbol.parameters) {
				expect(parameter.description, `${helper.name}: ${parameter.name}`).not.toMatch(
					/^The .+ value\.$/
				);
			}
			for (const option of symbol.options ?? []) {
				expect(option.description, `${helper.name}: ${option.name}`).toMatch(
					/^(Required|Optional)/
				);
			}
			expect(symbol.returns?.description, helper.name).not.toBe('The declared result.');
			expect(symbol.returns?.description.length, helper.name).toBeGreaterThan(40);
			expect(symbol.details.length, helper.name).toBeGreaterThan(2);
			expect(symbol.example, helper.name).toContain('import ');
			expect(symbol.example, helper.name).toContain(`${helper.name}(`);
			expect(symbol.relatedDocs.length, helper.name).toBeGreaterThan(0);
			expect(symbol.relatedSymbols.length, helper.name).toBeGreaterThan(1);
		}
	});

	test('documents the actual source options accepted by the registration helpers', () => {
		const configPath = path.join(repositoryRoot, 'packages/plugin/tsconfig.json');
		const config = ts.readConfigFile(configPath, ts.sys.readFile);
		if (config.error)
			throw new Error(ts.flattenDiagnosticMessageText(config.error.messageText, '\n'));
		const parsed = ts.parseJsonConfigFileContent(config.config, ts.sys, path.dirname(configPath));
		const program = ts.createProgram(parsed.fileNames, { ...parsed.options, noEmit: true });
		const checker = program.getTypeChecker();

		for (const helper of helpers) {
			const source = program.getSourceFile(path.join(repositoryRoot, helper.file));
			if (!source) throw new Error(`Missing source file ${helper.file}`);
			const declaration = source.statements.find(
				(statement): statement is ts.FunctionDeclaration =>
					ts.isFunctionDeclaration(statement) && statement.name?.text === helper.name
			);
			if (!declaration?.name) throw new Error(`Missing declaration ${helper.name}`);
			const signatures = checker.getTypeAtLocation(declaration.name).getCallSignatures();
			const sourceOptions = new Set<string>();
			for (const signature of signatures) {
				const argument = signature.getParameters()[0];
				if (!argument) continue;
				collectPropertyNames(
					checker.getTypeOfSymbolAtLocation(argument, declaration),
					checker,
					sourceOptions
				);
			}
			// The versioned helper adds this property; callers must not provide it.
			if (helper.name === 'defineAdminPlugin') sourceOptions.delete('apiVersion');
			const documented = referenceEditorialOverlays[helper.id]?.options?.map((item) => item.name);
			expect(documented?.toSorted(), helper.name).toEqual([...sourceOptions].sort());
		}
	});
});

function collectPropertyNames(
	type: ts.Type,
	checker: ts.TypeChecker,
	names: Set<string>,
	seen = new Set<ts.Type>()
): void {
	if (seen.has(type)) return;
	seen.add(type);
	for (const property of checker.getPropertiesOfType(type)) names.add(property.name);
	if (type.isUnionOrIntersection()) {
		for (const member of type.types) collectPropertyNames(member, checker, names, seen);
	}
	const constraint = checker.getBaseConstraintOfType(type);
	if (constraint && constraint !== type) collectPropertyNames(constraint, checker, names, seen);
}
