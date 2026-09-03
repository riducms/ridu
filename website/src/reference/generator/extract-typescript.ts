import { readFileSync } from 'node:fs';
import path from 'node:path';
import { svelte2tsx } from 'svelte2tsx';
import ts from 'typescript';
import type { TypeScriptReferenceEntrypoint } from '../authoring/modules';
import type { ReferenceKind, ReferenceParameter } from '../types';
import type { ExtractedDeclaration } from './types';

export function extractTypeScriptEntrypoint(
	repositoryRoot: string,
	entrypoint: TypeScriptReferenceEntrypoint
): ExtractedDeclaration[] {
	const entrypointPath = path.resolve(repositoryRoot, entrypoint.path);
	const configPath = path.resolve(repositoryRoot, entrypoint.tsconfig);
	const configFile = ts.readConfigFile(configPath, ts.sys.readFile);
	if (configFile.error) throw new Error(formatDiagnostic(configFile.error));
	const parsed = ts.parseJsonConfigFileContent(
		configFile.config,
		ts.sys,
		path.dirname(configPath),
		{ noEmit: true },
		configPath
	);
	if (parsed.errors.length > 0) throw new Error(parsed.errors.map(formatDiagnostic).join('\n'));

	const program = ts.createProgram({
		rootNames: [...new Set([...parsed.fileNames, entrypointPath])],
		options: parsed.options
	});
	const checker = program.getTypeChecker();
	const source = program.getSourceFile(entrypointPath);
	if (!source)
		throw new Error(`TypeScript reference entrypoint is not in the program: ${entrypoint.path}`);
	const moduleSymbol = checker.getSymbolAtLocation(source);
	if (!moduleSymbol)
		throw new Error(`TypeScript entrypoint has no module symbol: ${entrypoint.path}`);

	const declarations = checker
		.getExportsOfModule(moduleSymbol)
		.filter((symbol) => symbol.name !== 'default')
		.flatMap((symbol) => declarationForExport(repositoryRoot, entrypoint, checker, symbol));
	declarations.push(...extractSvelteReexports(repositoryRoot, entrypoint, source, parsed.options));
	return uniqueDeclarations(declarations);
}

function declarationForExport(
	repositoryRoot: string,
	entrypoint: TypeScriptReferenceEntrypoint,
	checker: ts.TypeChecker,
	exported: ts.Symbol
): ExtractedDeclaration[] {
	const target =
		exported.flags & ts.SymbolFlags.Alias ? checker.getAliasedSymbol(exported) : exported;
	const declaration =
		target.valueDeclaration ?? target.declarations?.[0] ?? exported.declarations?.[0];
	if (!declaration) return [];
	const sourceDeclaration =
		exported.declarations?.find(
			(candidate) => !candidate.getSourceFile().fileName.split(path.sep).includes('node_modules')
		) ?? declaration;
	const name = exported.name;
	const kind = declarationKind(target, declaration);
	const type = typeForSymbol(checker, target, declaration);
	const signatures = type.getCallSignatures();
	const overloads = signatures.map((signature) =>
		callSignature(checker, name, signature, kind === 'method' ? 'method' : 'function')
	);
	const signature = declarationSignature(checker, name, target, declaration, kind, type, overloads);
	const summary = ts.displayPartsToString(target.getDocumentationComment(checker)).trim();
	const parameters = signatures[0] ? signatureParameters(checker, signatures[0], declaration) : [];
	const returns = signatures[0]
		? checker.typeToString(signatures[0].getReturnType(), declaration, typeFormatFlags)
		: undefined;
	const members =
		ts.isInterfaceDeclaration(declaration) ||
		ts.isClassDeclaration(declaration) ||
		(ts.isTypeAliasDeclaration(declaration) && ts.isTypeLiteralNode(declaration.type))
			? typeMembers(checker, type, declaration)
			: [];
	const id = `ts:${entrypoint.specifier}#${name}`;
	const result: ExtractedDeclaration[] = [
		{
			id,
			name,
			kind,
			signature,
			summary,
			source: sourceLocation(repositoryRoot, sourceDeclaration),
			members,
			parameters,
			...(returns ? { returns } : {}),
			aliases: [name, `${entrypoint.specifier}.${name}`],
			overloads
		}
	];

	if (['class', 'interface'].includes(kind)) {
		for (const member of checker.getPropertiesOfType(type)) {
			const memberDeclaration = member.valueDeclaration ?? member.declarations?.[0];
			if (!memberDeclaration) continue;
			if (
				ts.getCombinedModifierFlags(memberDeclaration as ts.Declaration) &
				(ts.ModifierFlags.Private | ts.ModifierFlags.Protected)
			) {
				continue;
			}
			const memberType = checker.getTypeOfSymbolAtLocation(member, memberDeclaration);
			const memberSignatures = memberType.getCallSignatures();
			if (memberSignatures.length === 0) continue;
			const memberName = `${name}.${member.name}`;
			const memberOverloads = memberSignatures.map((memberSignature) =>
				callSignature(checker, memberName, memberSignature, 'method')
			);
			result.push({
				id: `ts:${entrypoint.specifier}#${memberName}`,
				name: memberName,
				kind: 'method',
				signature: memberOverloads.join('\n'),
				summary: ts.displayPartsToString(member.getDocumentationComment(checker)).trim(),
				source: sourceLocation(repositoryRoot, memberDeclaration),
				members: [],
				parameters: signatureParameters(checker, memberSignatures[0], memberDeclaration),
				returns: checker.typeToString(
					memberSignatures[0].getReturnType(),
					memberDeclaration,
					typeFormatFlags
				),
				receiver: name,
				aliases: [memberName, `${entrypoint.specifier}.${memberName}`],
				overloads: memberOverloads
			});
		}
	}

	return result;
}

function declarationKind(symbol: ts.Symbol, declaration: ts.Declaration): ReferenceKind {
	if (ts.isClassDeclaration(declaration)) return 'class';
	if (ts.isInterfaceDeclaration(declaration)) return 'interface';
	if (ts.isTypeAliasDeclaration(declaration)) return 'type';
	if (ts.isFunctionDeclaration(declaration) || symbol.flags & ts.SymbolFlags.Function)
		return 'function';
	if (symbol.flags & ts.SymbolFlags.ConstEnum) return 'type';
	if (symbol.flags & ts.SymbolFlags.Variable) {
		const declarationList = declaration.parent?.parent;
		return declarationList && ts.isVariableStatement(declarationList)
			? declarationList.declarationList.flags & ts.NodeFlags.Const
				? 'constant'
				: 'variable'
			: 'variable';
	}
	return 'type';
}

function declarationSignature(
	checker: ts.TypeChecker,
	name: string,
	symbol: ts.Symbol,
	declaration: ts.Declaration,
	kind: ReferenceKind,
	type: ts.Type,
	overloads: string[]
): string {
	if (overloads.length > 0) return overloads.join('\n');
	if (ts.isTypeAliasDeclaration(declaration)) {
		return `type ${name}${declaration.typeParameters?.map((parameter) => parameter.getText()).join(', ') ? `<${declaration.typeParameters?.map((parameter) => parameter.getText()).join(', ')}>` : ''} = ${declaration.type.getText()}`;
	}
	if (ts.isInterfaceDeclaration(declaration) || ts.isClassDeclaration(declaration)) {
		const keyword = ts.isInterfaceDeclaration(declaration) ? 'interface' : 'class';
		const parameters = declaration.typeParameters?.length
			? `<${declaration.typeParameters.map((parameter) => parameter.getText()).join(', ')}>`
			: '';
		return `${keyword} ${name}${parameters} { ... }`;
	}
	const typeText = compactTypeText(
		checker,
		declaration,
		checker.typeToString(type, declaration, typeFormatFlags)
	);
	if (kind === 'constant') return `const ${name}: ${typeText}`;
	if (kind === 'variable') return `let ${name}: ${typeText}`;
	if (symbol.flags & ts.SymbolFlags.Enum) return `enum ${name} { ... }`;
	return `type ${name} = ${typeText}`;
}

function compactTypeText(
	checker: ts.TypeChecker,
	declaration: ts.Declaration,
	typeText: string
): string {
	if (typeText.length <= 180 || !ts.isVariableDeclaration(declaration)) return typeText;
	if (declaration.type) return declaration.type.getText();
	const satisfies = satisfiesType(declaration.initializer);
	if (satisfies) return satisfies;
	const call = callInitializer(declaration.initializer);
	if (call) return `ReturnType<typeof ${call.expression.getText()}>`;
	const object = objectInitializer(declaration.initializer);
	if (object) {
		const properties = object.properties.flatMap((property): string[] => {
			if (!ts.isPropertyAssignment(property) && !ts.isShorthandPropertyAssignment(property))
				return [];
			const name = property.name.getText();
			const initializer = ts.isShorthandPropertyAssignment(property)
				? property.name
				: property.initializer;
			if (ts.isIdentifier(initializer)) return [`readonly ${name}: typeof ${initializer.text}`];
			if (
				ts.isStringLiteral(initializer) ||
				ts.isNumericLiteral(initializer) ||
				initializer.kind === ts.SyntaxKind.TrueKeyword ||
				initializer.kind === ts.SyntaxKind.FalseKeyword
			) {
				return [`readonly ${name}: ${initializer.getText()}`];
			}
			const inferred = checker.typeToString(
				checker.getTypeAtLocation(initializer),
				initializer,
				typeFormatFlags
			);
			return [`readonly ${name}: ${inferred.length <= 80 ? inferred : 'unknown'}`];
		});
		if (properties.length > 0 && properties.length <= 12) return `{ ${properties.join('; ')} }`;
	}
	return checker.typeToString(checker.getTypeAtLocation(declaration), declaration);
}

function satisfiesType(expression: ts.Expression | undefined): string | undefined {
	if (!expression) return undefined;
	if (ts.isSatisfiesExpression(expression)) return expression.type.getText();
	if (ts.isAsExpression(expression) || ts.isParenthesizedExpression(expression)) {
		return satisfiesType(expression.expression);
	}
	return undefined;
}

function objectInitializer(
	expression: ts.Expression | undefined
): ts.ObjectLiteralExpression | undefined {
	if (!expression) return undefined;
	if (ts.isObjectLiteralExpression(expression)) return expression;
	if (
		ts.isAsExpression(expression) ||
		ts.isSatisfiesExpression(expression) ||
		ts.isParenthesizedExpression(expression)
	) {
		return objectInitializer(expression.expression);
	}
	return undefined;
}

function callInitializer(expression: ts.Expression | undefined): ts.CallExpression | undefined {
	if (!expression) return undefined;
	if (ts.isCallExpression(expression)) return expression;
	if (
		ts.isAsExpression(expression) ||
		ts.isSatisfiesExpression(expression) ||
		ts.isParenthesizedExpression(expression)
	) {
		return callInitializer(expression.expression);
	}
	return undefined;
}

function typeForSymbol(
	checker: ts.TypeChecker,
	symbol: ts.Symbol,
	declaration: ts.Declaration
): ts.Type {
	if (symbol.flags & (ts.SymbolFlags.Interface | ts.SymbolFlags.Class | ts.SymbolFlags.TypeAlias)) {
		return checker.getDeclaredTypeOfSymbol(symbol);
	}
	return checker.getTypeOfSymbolAtLocation(symbol, declaration);
}

function callSignature(
	checker: ts.TypeChecker,
	name: string,
	signature: ts.Signature,
	kind: 'function' | 'method'
): string {
	const rendered = checker.signatureToString(
		signature,
		signature.declaration,
		typeFormatFlags,
		ts.SignatureKind.Call
	);
	return `${kind === 'method' ? '' : 'function '}${name}${rendered}`;
}

function signatureParameters(
	checker: ts.TypeChecker,
	signature: ts.Signature,
	location: ts.Node
): ReferenceParameter[] {
	return signature.parameters.map((parameter) => {
		const declaration = parameter.valueDeclaration ?? parameter.declarations?.[0] ?? location;
		return {
			name: parameter.name,
			type: checker.typeToString(
				checker.getTypeOfSymbolAtLocation(parameter, declaration),
				declaration,
				typeFormatFlags
			),
			description: ts.displayPartsToString(parameter.getDocumentationComment(checker)).trim()
		};
	});
}

function typeMembers(
	checker: ts.TypeChecker,
	type: ts.Type,
	location: ts.Node
): ReferenceParameter[] {
	return checker.getPropertiesOfType(type).map((member) => {
		const declaration = member.valueDeclaration ?? member.declarations?.[0] ?? location;
		return {
			name: member.name,
			type: checker.typeToString(
				checker.getTypeOfSymbolAtLocation(member, declaration),
				declaration,
				typeFormatFlags
			),
			description: ts.displayPartsToString(member.getDocumentationComment(checker)).trim()
		};
	});
}

function extractSvelteReexports(
	repositoryRoot: string,
	entrypoint: TypeScriptReferenceEntrypoint,
	source: ts.SourceFile,
	compilerOptions: ts.CompilerOptions
): ExtractedDeclaration[] {
	const declarations: ExtractedDeclaration[] = [];
	for (const statement of source.statements) {
		if (
			!ts.isExportDeclaration(statement) ||
			!statement.moduleSpecifier ||
			!ts.isStringLiteral(statement.moduleSpecifier) ||
			!statement.moduleSpecifier.text.endsWith('.svelte') ||
			!statement.exportClause ||
			!ts.isNamedExports(statement.exportClause)
		) {
			continue;
		}
		const sveltePath = resolveSveltePath(
			repositoryRoot,
			entrypoint,
			source.fileName,
			statement.moduleSpecifier.text,
			compilerOptions
		);
		const sourceText = readFileSync(sveltePath, 'utf8');
		const transformed = svelte2tsx(sourceText, {
			filename: sveltePath,
			isTsFile: true,
			mode: 'ts'
		}).code;
		const propsType = transformed.match(/return \{ props: \{\} as any as ([^,}]+)/)?.[1]?.trim();
		for (const element of statement.exportClause.elements) {
			if (element.propertyName?.text !== 'default') continue;
			const name = element.name.text;
			declarations.push({
				id: `ts:${entrypoint.specifier}#${name}`,
				name,
				kind: 'component',
				signature: `component ${name}(props: ${propsType ?? 'Record<string, unknown>'})`,
				summary: `Public Svelte component exported as ${name}.`,
				source: sourceLocationFromPath(repositoryRoot, sveltePath, 1),
				members: propsType ? membersFromSvelteScript(sourceText, propsType) : [],
				parameters: [],
				aliases: [name, `${entrypoint.specifier}.${name}`],
				overloads: []
			});
		}
	}
	return declarations;
}

function membersFromSvelteScript(source: string, typeName: string): ReferenceParameter[] {
	const script = source.match(/<script(?:\s+lang=["']ts["'])?[^>]*>([\s\S]*?)<\/script>/)?.[1];
	if (!script) return [];
	const file = ts.createSourceFile(
		'component.ts',
		script,
		ts.ScriptTarget.Latest,
		true,
		ts.ScriptKind.TS
	);
	for (const statement of file.statements) {
		if (
			(ts.isInterfaceDeclaration(statement) || ts.isTypeAliasDeclaration(statement)) &&
			statement.name.text === typeName
		) {
			const members = ts.isInterfaceDeclaration(statement)
				? statement.members
				: ts.isTypeLiteralNode(statement.type)
					? statement.type.members
					: [];
			return members.flatMap((member): ReferenceParameter[] => {
				if (
					!ts.isPropertySignature(member) &&
					!ts.isMethodSignature(member) &&
					!ts.isCallSignatureDeclaration(member)
				) {
					return [];
				}
				if (!member.name || !member.type) return [];
				return [
					{
						name: member.name.getText(file),
						type: member.type.getText(file),
						description: jsDocText(member)
					}
				];
			});
		}
	}
	return [];
}

function resolveSveltePath(
	repositoryRoot: string,
	entrypoint: TypeScriptReferenceEntrypoint,
	from: string,
	specifier: string,
	options: ts.CompilerOptions
): string {
	if (specifier.startsWith('.')) return path.resolve(path.dirname(from), specifier);
	for (const [pattern, replacements] of Object.entries(options.paths ?? {})) {
		const wildcard = pattern.indexOf('*');
		const prefix = wildcard >= 0 ? pattern.slice(0, wildcard) : pattern;
		const suffix = wildcard >= 0 ? pattern.slice(wildcard + 1) : '';
		if (!specifier.startsWith(prefix) || !specifier.endsWith(suffix)) continue;
		const matched = specifier.slice(prefix.length, specifier.length - suffix.length);
		for (const replacement of replacements) {
			const candidate = replacement.replace('*', matched);
			const configDirectory = path.resolve(repositoryRoot, path.dirname(entrypoint.tsconfig));
			return path.resolve(configDirectory, candidate);
		}
	}
	throw new Error(
		`cannot resolve Svelte export ${specifier} from ${entrypoint.path} in ${repositoryRoot}`
	);
}

function sourceLocation(repositoryRoot: string, declaration: ts.Declaration) {
	const source = declaration.getSourceFile();
	return sourceLocationFromPath(
		repositoryRoot,
		source.fileName,
		source.getLineAndCharacterOfPosition(declaration.getStart(source)).line + 1
	);
}

function sourceLocationFromPath(repositoryRoot: string, sourcePath: string, line: number) {
	return { path: path.relative(repositoryRoot, sourcePath).replaceAll(path.sep, '/'), line };
}

function jsDocText(node: ts.Node): string {
	const docs = (node as ts.Node & { jsDoc?: readonly ts.JSDoc[] }).jsDoc;
	return (
		docs
			?.flatMap((doc: ts.JSDoc) => (typeof doc.comment === 'string' ? [doc.comment] : []))
			.join(' ')
			.trim() ?? ''
	);
}

function uniqueDeclarations(declarations: ExtractedDeclaration[]): ExtractedDeclaration[] {
	const unique = new Map<string, ExtractedDeclaration>();
	for (const declaration of declarations) {
		const existing = unique.get(declaration.id);
		if (!existing || existing.kind !== 'component') unique.set(declaration.id, declaration);
	}
	return [...unique.values()].sort((left, right) => left.id.localeCompare(right.id));
}

function formatDiagnostic(diagnostic: ts.Diagnostic): string {
	return ts.flattenDiagnosticMessageText(diagnostic.messageText, '\n');
}

const typeFormatFlags =
	ts.TypeFormatFlags.NoTruncation |
	ts.TypeFormatFlags.UseAliasDefinedOutsideCurrentScope |
	ts.TypeFormatFlags.WriteTypeArgumentsOfSignature;
