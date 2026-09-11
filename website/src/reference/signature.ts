import type { ReferenceParameter } from './types';

export type SignatureTokenKind =
	| 'space'
	| 'parameter'
	| 'symbol'
	| 'reference'
	| 'keyword'
	| 'comment'
	| 'string'
	| 'number'
	| 'type'
	| 'punctuation'
	| 'plain';

export interface SignatureToken {
	text: string;
	kind: SignatureTokenKind;
	href?: string;
}

export interface SignatureTokenOptions {
	name: string;
	parameters?: Pick<ReferenceParameter, 'name'>[];
	typeLinks?: Record<string, string>;
}

export function splitReferenceType(value: string): string[] {
	return value.match(/[A-Za-z_$@][\w$@]*(?:\.[A-Za-z_$][\w$]*)*|\s+|./g) ?? [value];
}

const keywords = new Set([
	'async',
	'class',
	'const',
	'declare',
	'export',
	'extends',
	'func',
	'function',
	'interface',
	'map',
	'new',
	'readonly',
	'struct',
	'type',
	'var'
]);

const builtins = new Set([
	'any',
	'bool',
	'boolean',
	'byte',
	'error',
	'int',
	'int64',
	'never',
	'number',
	'string',
	'unknown',
	'void'
]);

const splitTopLevel = (value: string, delimiter: ',' | ';' | '|') => {
	const parts: string[] = [];
	let start = 0;
	let round = 0;
	let square = 0;
	let curly = 0;
	let angle = 0;
	let quote = '';

	for (let index = 0; index < value.length; index += 1) {
		const character = value[index];
		const previous = value[index - 1];

		if (quote) {
			if (character === quote && previous !== '\\') quote = '';
			continue;
		}

		if (character === '"' || character === "'" || character === '`') {
			quote = character;
			continue;
		}

		if (character === '(') round += 1;
		else if (character === ')') round -= 1;
		else if (character === '[') square += 1;
		else if (character === ']') square -= 1;
		else if (character === '{') curly += 1;
		else if (character === '}') curly -= 1;
		else if (character === '<') angle += 1;
		else if (character === '>' && angle > 0 && previous !== '=') angle -= 1;

		if (character === delimiter && round === 0 && square === 0 && curly === 0 && angle === 0) {
			parts.push(value.slice(start, index).trim());
			start = index + 1;
		}
	}

	parts.push(value.slice(start).trim());
	return parts.filter(Boolean);
};

const splitTopLevelWhitespace = (value: string): string[] => {
	const parts: string[] = [];
	let start = 0;
	let round = 0;
	let square = 0;
	let curly = 0;
	let angle = 0;
	let quote = '';

	for (let index = 0; index < value.length; index += 1) {
		const character = value[index];
		const previous = value[index - 1];

		if (quote) {
			if (character === quote && previous !== '\\') quote = '';
			continue;
		}
		if (character === '"' || character === "'" || character === '`') {
			quote = character;
			continue;
		}
		if (character === '(') round += 1;
		else if (character === ')') round -= 1;
		else if (character === '[') square += 1;
		else if (character === ']') square -= 1;
		else if (character === '{') curly += 1;
		else if (character === '}') curly -= 1;
		else if (character === '<') angle += 1;
		else if (character === '>' && angle > 0 && previous !== '=') angle -= 1;

		if (/\s/.test(character) && round === 0 && square === 0 && curly === 0 && angle === 0) {
			const part = value.slice(start, index).trim();
			if (part) parts.push(part);
			start = index + 1;
		}
	}

	const tail = value.slice(start).trim();
	if (tail) parts.push(tail);
	return parts;
};

const findMatching = (value: string, start: number, open: string, close: string) => {
	let depth = 0;
	for (let index = start; index < value.length; index += 1) {
		if (value[index] === open) depth += 1;
		if (value[index] === close) depth -= 1;
		if (depth === 0) return index;
	}
	return -1;
};

const findTopLevel = (value: string, target: string, start = 0): number => {
	let round = 0;
	let square = 0;
	let curly = 0;
	let angle = 0;
	let quote = '';

	for (let index = start; index < value.length; index += 1) {
		const character = value[index];
		const previous = value[index - 1];
		if (quote) {
			if (character === quote && previous !== '\\') quote = '';
			continue;
		}
		if (character === '"' || character === "'" || character === '`') {
			quote = character;
			continue;
		}

		if (character === target && round === 0 && square === 0 && curly === 0 && angle === 0) {
			return index;
		}
		if (character === '(') round += 1;
		else if (character === ')') round -= 1;
		else if (character === '[') square += 1;
		else if (character === ']') square -= 1;
		else if (character === '{') curly += 1;
		else if (character === '}') curly -= 1;
		else if (character === '<') angle += 1;
		else if (character === '>' && angle > 0 && previous !== '=') angle -= 1;
	}
	return -1;
};

const formatDeclarationGroup = (value: string): string | undefined => {
	const match = value.match(/^(const|var|type)\s*\(/);
	if (!match) return undefined;
	const start = value.indexOf('(', match[1].length);
	const end = findMatching(value, start, '(', ')');
	if (end < 0 || value.slice(end + 1).trim()) return undefined;
	const declarations = splitTopLevel(value.slice(start + 1, end), ';');
	if (declarations.length < 2) return undefined;
	return `${value.slice(0, start + 1)}\n${declarations
		.map((declaration) => `\t${declaration}`)
		.join('\n')}\n)`;
};

const formatStatementList = (value: string): string | undefined => {
	const statements = splitTopLevel(value, ';');
	if (statements.length < 2 || value.length <= 82) return undefined;
	return statements
		.map((statement) => `${statement};`)
		.join('\n')
		.replace(/;$/, '');
};

const formatVariableList = (value: string): string | undefined => {
	if (!value.startsWith('var ') || value.length <= 82) return undefined;
	const declaration = value.slice(4);
	const match = declaration.match(/^((?:[A-Za-z_$][\w$]*\s*,\s*)+)([A-Za-z_$][\w$]*)\s+(.+)$/s);
	if (!match) return undefined;
	const names = [...splitTopLevel(match[1].replace(/,\s*$/, ''), ','), match[2]];
	const type = match[3].trim();
	if (names.length < 2 || !type) return undefined;
	return `var (\n${names.map((name) => `\t${name} ${type}`).join('\n')}\n)`;
};

const formatCommand = (value: string): string | undefined => {
	if (value.length <= 82 || !/^(?:ridu\b|--)/.test(value)) return undefined;
	const tokens = splitTopLevelWhitespace(value);
	const head: string[] = [];
	const options: string[] = [];
	for (const token of tokens) {
		if (token.startsWith('--') || token.startsWith('[')) {
			options.push(token);
		} else if (options.length > 0) {
			options[options.length - 1] += ` ${token}`;
		} else {
			head.push(token);
		}
	}
	if (options.length < 2) return undefined;
	const continuationIndent = head.length > 0 ? '\t' : '';
	return [head.join(' '), ...options.map((option) => `${continuationIndent}${option}`)]
		.filter(Boolean)
		.join('\n');
};

const formatGenericDeclaration = (value: string): string => {
	const match = value.match(
		/^(?:(?:function|func|type|interface|class)\s+)?[A-Za-z_$][\w$.[\]]*\s*([<[])/
	);
	const start = match ? value.indexOf(match[1], match.index ?? 0) : -1;
	if (start < 0) return value;
	const open = value[start];
	const close = open === '[' ? ']' : '>';
	const end = findMatching(value, start, open, close);
	if (end < 0) return value;
	const parameters = splitTopLevel(value.slice(start + 1, end), ',');
	if (parameters.length < 2 && value.length <= 82) return value;
	return `${value.slice(0, start + 1)}\n${parameters
		.map((parameter) => {
			const formatted = formatGenericExpression(formatInlineUnion(parameter, 2), 1);
			return `\t${formatted.replaceAll('\n', '\n\t')},`;
		})
		.join('\n')}\n${value.slice(end)}`;
};

const formatConditional = (value: string): string => {
	const question = findTopLevel(value, '?');
	if (question < 0) return value;
	const colon = findTopLevel(value, ':', question + 1);
	if (colon < 0) return value;
	return `${value.slice(0, question).trim()}\n\t? ${value.slice(question + 1, colon).trim()}\n\t: ${value.slice(colon + 1).trim()}`;
};

const formatInlineUnion = (value: string, indent: number): string => {
	if (value.length <= 82) return value;
	const members = splitTopLevel(value, '|');
	if (members.length < 2) return value;
	return `${members[0]}\n${members
		.slice(1)
		.map((member) => `${'\t'.repeat(indent)}| ${member}`)
		.join('\n')}`;
};

const formatGenericExpression = (value: string, indent: number): string => {
	if (value.length <= 82) return value;
	const start = value.indexOf('<');
	if (start < 0) return value;
	const end = findMatching(value, start, '<', '>');
	if (end < 0) return value;
	const argumentsList = splitTopLevel(value.slice(start + 1, end), ',');
	const formattedArguments = argumentsList.map((argument) =>
		formatInlineUnion(argument, indent + 1)
	);
	const separator = argumentsList.length > 1 ? ',' : '';
	return `${value.slice(0, start + 1)}\n${argumentsList
		.map((_argument, index) => `${'\t'.repeat(indent + 1)}${formattedArguments[index]}${separator}`)
		.join('\n')}\n${'\t'.repeat(indent)}${value.slice(end)}`;
};

const formatCallableExpression = (value: string, indent: number): string => {
	if (value.length <= 82) return value;
	const start = value.indexOf('(');
	if (start < 0) return value;
	const end = findMatching(value, start, '(', ')');
	if (end < 0) return value;
	const parameters = splitTopLevel(value.slice(start + 1, end), ',');
	if (parameters.length < 2) return value;
	return `${value.slice(0, start + 1)}\n${parameters
		.map(
			(parameter) => `${'\t'.repeat(indent + 1)}${formatGenericExpression(parameter, indent + 1)},`
		)
		.join('\n')}\n${'\t'.repeat(indent)}${value.slice(end)}`;
};

const formatBracedExpression = (value: string, indent = 0): string => {
	const start = value.indexOf('{');
	if (start < 0) {
		return formatInlineUnion(formatCallableExpression(value, indent), indent);
	}
	const end = findMatching(value, start, '{', '}');
	if (end < 0) return value;

	const members = splitTopLevel(value.slice(start + 1, end), ';');
	const formattedMembers = members.map((member) => formatBracedExpression(member, indent + 1));
	const trailing = formatBracedExpression(value.slice(end + 1), indent);
	const shouldExpand =
		members.length > 1 ||
		formattedMembers.some((member) => member.includes('\n')) ||
		value.length > 82;
	if (!shouldExpand) {
		return `${value.slice(0, start + 1)}${value.slice(start + 1, end)}}${trailing}`;
	}

	const memberIndent = '\t'.repeat(indent + 1);
	const closingIndent = '\t'.repeat(indent);
	return `${value.slice(0, start + 1)}\n${formattedMembers
		.map((member) => `${memberIndent}${member}`)
		.join('\n')}\n${closingIndent}}${trailing}`;
};

const formatTypeAlias = (value: string): string | undefined => {
	if (!/^type\s/.test(value) || value.length <= 82) return undefined;
	const equals = findTopLevel(value, '=');
	if (equals < 0) return undefined;
	const left = formatGenericDeclaration(value.slice(0, equals).trim());
	const right = value.slice(equals + 1).trim();
	const union = splitTopLevel(right, '|');
	if (union.length > 1) {
		return `${left} =\n${union
			.map((member) => {
				const formatted = formatBracedExpression(member).split('\n');
				return [`\t| ${formatted[0]}`, ...formatted.slice(1).map((line) => `\t${line}`)].join('\n');
			})
			.join('\n')}`;
	}
	const conditional = formatConditional(formatBracedExpression(right));
	if (left.includes('\n') || conditional.includes('\n') || value.length > 100) {
		return `${left} =\n${conditional
			.split('\n')
			.map((line) => `\t${line}`)
			.join('\n')}`;
	}
	return undefined;
};

const genericDeclarationEnd = (value: string): number => {
	const match = value.match(
		/^(?:(?:function|func|type|interface|class)\s+)?[A-Za-z_$][\w$.[\]]*\s*([<[])/
	);
	if (!match) return -1;
	const start = value.indexOf(match[1], match.index ?? 0);
	return findMatching(value, start, value[start], value[start] === '[' ? ']' : '>');
};

const formatCallableDeclaration = (value: string): string | undefined => {
	let parametersStart = value.indexOf('(');
	if (parametersStart < 0) return undefined;
	const braceStart = value.indexOf('{');
	if (braceStart >= 0 && braceStart < parametersStart) return undefined;
	if (/^func\s*\(/.test(value)) {
		const receiverEnd = findMatching(value, parametersStart, '(', ')');
		parametersStart = receiverEnd < 0 ? -1 : value.indexOf('(', receiverEnd + 1);
		if (parametersStart < 0) return undefined;
	}
	const parametersEnd = findMatching(value, parametersStart, '(', ')');
	if (parametersEnd < 0) return undefined;
	const parameterSource = value.slice(parametersStart + 1, parametersEnd).trim();
	if (parameterSource) {
		const declarationParameters = splitTopLevel(parameterSource, ',');
		if (declarationParameters.length < 2 && value.length <= 82) return undefined;
		return `${value.slice(0, parametersStart + 1)}\n${declarationParameters
			.map((parameter) => `\t${formatBracedExpression(formatGenericExpression(parameter, 1), 1)},`)
			.join('\n')}\n${value.slice(parametersEnd)}`;
	}

	const resultsStart = value.indexOf('(', parametersEnd + 1);
	if (resultsStart < 0) return undefined;
	const resultsEnd = findMatching(value, resultsStart, '(', ')');
	if (resultsEnd < 0) return undefined;
	const results = splitTopLevel(value.slice(resultsStart + 1, resultsEnd), ',');
	if (results.length < 2) return undefined;
	return `${value.slice(0, resultsStart + 1)}\n${results.map((result) => `\t${result},`).join('\n')}\n${value.slice(resultsEnd)}`;
};

export function formatReferenceSignature(value: string): string {
	if (value.includes('\n')) {
		return value
			.split('\n')
			.map((line) => {
				if (line.length <= 100 || /^\s*\/\//.test(line)) return line;
				const indent = line.match(/^\s*/)?.[0] ?? '';
				return formatReferenceSignature(line.trimStart())
					.split('\n')
					.map((formatted) => `${indent}${formatted}`)
					.join('\n');
			})
			.join('\n');
	}

	const declarationGroup = formatDeclarationGroup(value);
	if (declarationGroup) return declarationGroup;
	const statementList = formatStatementList(value);
	if (statementList) return statementList;
	const variableList = formatVariableList(value);
	if (variableList) return variableList;
	const command = formatCommand(value);
	if (command) return command;
	const typeAlias = formatTypeAlias(value);
	if (typeAlias) return typeAlias;
	if (value.length > 100 && genericDeclarationEnd(value) > 100) {
		const generic = formatGenericDeclaration(value);
		if (generic !== value && generic.includes('\n')) return formatReferenceSignature(generic);
	}
	const callable = formatCallableDeclaration(value);
	if (callable) return formatReferenceSignature(callable);
	const bracedExpression = formatBracedExpression(value);
	if (bracedExpression.includes('\n')) return bracedExpression;
	const genericExpression = formatGenericExpression(value, 0);
	if (genericExpression.includes('\n')) return formatReferenceSignature(genericExpression);
	return formatInlineUnion(value, 1);
}

export function tokenizeReferenceSignature(
	signature: string,
	{ name, parameters = [], typeLinks = {} }: SignatureTokenOptions
): SignatureToken[] {
	const parameterNames = new Set(
		parameters.map((parameter) => parameter.name.replace(/^\.\.\./, ''))
	);
	const symbolNames = new Set([name, name.split('.').at(-1) ?? name]);
	const rawTokens =
		signature.match(
			/(?:(?:"(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|`(?:\\.|[^`\\])*`)|\/\/[^\r\n]*|\/\*[\s\S]*?\*\/|\.\.\.|=>|:=|[A-Za-z_$@][\w$@]*(?:\.[A-Za-z_$][\w$]*)*|\d+(?:\.\d+)?|\s+|.)/gs
		) ?? [];

	let startsDeclarationLine = false;
	return rawTokens.map((text): SignatureToken => {
		const clean = text.replace(/^\.\.\./, '');
		if (text.startsWith('//') || text.startsWith('/*')) {
			startsDeclarationLine = false;
			return { text, kind: 'comment' };
		}
		if (/^\s+$/.test(text)) {
			if (text.includes('\n')) startsDeclarationLine = true;
			return { text, kind: 'space' };
		}

		const isDeclaredMember = startsDeclarationLine && parameterNames.has(clean);
		startsDeclarationLine = false;
		if (isDeclaredMember) return { text, kind: 'parameter' };

		const directHref = Object.hasOwn(typeLinks, text) ? typeLinks[text] : undefined;
		const cleanHref = Object.hasOwn(typeLinks, clean) ? typeLinks[clean] : undefined;
		const href = directHref ?? cleanHref;
		if (href) {
			return { text, href, kind: symbolNames.has(clean) ? 'symbol' : 'reference' };
		}
		if (symbolNames.has(clean)) return { text, kind: 'symbol' };
		if (keywords.has(text)) return { text, kind: 'keyword' };
		if (parameterNames.has(clean)) return { text, kind: 'parameter' };
		if (/^["'`]/.test(text)) return { text, kind: 'string' };
		if (/^\d/.test(text)) return { text, kind: 'number' };
		if (builtins.has(text) || /^[A-Z]/.test(text) || /^[a-z_$][\w$]*\.[A-Z]/.test(text)) {
			return { text, kind: 'type' };
		}
		if (/^[()[\]{}<>,.:;?*|&=+\-]+$/.test(text)) return { text, kind: 'punctuation' };
		return { text, kind: 'plain' };
	});
}
