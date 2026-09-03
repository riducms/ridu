/// <reference types="bun-types" />

import { expect, test } from 'bun:test';
import { findReferenceModule, findReferenceSymbol } from '@/reference';
import { buildReferenceSearchText } from './text';

test('reference search includes public members, their types, and descriptions', () => {
	const module = findReferenceModule('ridu');
	const handler = findReferenceSymbol('ridu', 'handler-options');
	expect(module).toBeDefined();
	expect(handler).toBeDefined();
	if (!module || !handler) return;

	const text = buildReferenceSearchText(module, handler);
	expect(text).toContain('allowedrequestheaders');
	expect(text).toContain('application-owned cors request headers');
});

test('reference search exposes exact runtime constants and receiver methods', () => {
	const frameworkVersion = findReferenceSymbol('ridu', 'framework-version');
	const bulkUpdate = findReferenceSymbol('core', 'local-api-bulk-update');
	expect(frameworkVersion).toBeDefined();
	expect(bulkUpdate).toBeDefined();
	if (!frameworkVersion || !bulkUpdate) return;

	const ridu = findReferenceModule('ridu');
	const core = findReferenceModule('core');
	expect(ridu && buildReferenceSearchText(ridu, frameworkVersion)).toContain('frameworkversion');
	expect(core && buildReferenceSearchText(core, bulkUpdate)).toContain('localapi.bulkupdate');
});

test('SDK internal helper names lead to their public consuming methods', () => {
	const sdk = findReferenceModule('sdk');
	expect(sdk).toBeDefined();
	if (!sdk) return;

	for (const [slug, helper] of [
		['ridu-client-create-a-p-i-key', 'CreateAPIKeyInput'],
		['ridu-client-collection-access', 'CollectionAccessOptions'],
		['ridu-client-global-access', 'GlobalAccessOptions']
	] as const) {
		const method = findReferenceSymbol('sdk', slug);
		expect(method, `${helper} consuming method is missing`).toBeDefined();
		expect(method && buildReferenceSearchText(sdk, method)).toContain(helper.toLocaleLowerCase());
	}
});

test('exported Go enum identifiers and literals are directly searchable', () => {
	for (const [moduleSlug, symbolSlug, identifier, literal] of [
		['schema', 'field-type', 'FieldTypeText', '"text"'],
		['store', 'task-state', 'TaskStateQueued', '"queued"'],
		['migration', 'phase-mode', 'PhaseTransaction', '"transaction"'],
		['postgres', 'migration-notice', 'NoticeAtlasProvenance', '"RIDU_ATLAS_PROVENANCE"'],
		['query', 'operator', 'OperatorNotEqual', '"not_equal"']
	] as const) {
		const module = findReferenceModule(moduleSlug);
		const symbol = findReferenceSymbol(moduleSlug, symbolSlug);
		expect(module).toBeDefined();
		expect(symbol).toBeDefined();
		if (!module || !symbol) continue;
		const text = buildReferenceSearchText(module, symbol);
		expect(text).toContain(identifier.toLocaleLowerCase());
		expect(text).toContain(literal.toLocaleLowerCase());
	}
});
