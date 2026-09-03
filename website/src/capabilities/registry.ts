import registryData from './registry.json';

export const capabilityStatuses = ['available', 'limited', 'experimental', 'planned'] as const;
export type CapabilityStatus = (typeof capabilityStatuses)[number];

export interface CapabilitySetupCoverage {
	newProject: string;
	existingProject: string;
}

export interface Capability {
	id: string;
	name: string;
	category: string;
	status: CapabilityStatus;
	summary: string;
	qualificationLimits: string[];
	owners: string[];
	evidence: string[];
	adapters: string[];
	guides: string[];
	symbols: string[];
	setupCoverage?: CapabilitySetupCoverage;
}

const isStringArray = (value: unknown): value is string[] =>
	Array.isArray(value) && value.every((entry) => typeof entry === 'string' && entry.length > 0);

function parseCapability(value: unknown, index: number): Capability {
	if (!value || typeof value !== 'object') {
		throw new Error(`Capability registry entry ${index} must be an object`);
	}
	const entry = value as Record<string, unknown>;
	for (const key of ['id', 'name', 'category', 'status', 'summary'] as const) {
		if (typeof entry[key] !== 'string' || entry[key].length === 0) {
			throw new Error(`Capability registry entry ${index} has an invalid ${key}`);
		}
	}
	if (!capabilityStatuses.includes(entry.status as CapabilityStatus)) {
		throw new Error(`Capability ${entry.id} has unsupported status ${String(entry.status)}`);
	}
	for (const key of [
		'qualificationLimits',
		'owners',
		'evidence',
		'adapters',
		'guides',
		'symbols'
	] as const) {
		if (
			!isStringArray(entry[key]) &&
			!(key === 'qualificationLimits' && Array.isArray(entry[key]))
		) {
			throw new Error(`Capability ${entry.id} has an invalid ${key}`);
		}
	}
	let setupCoverage: CapabilitySetupCoverage | undefined;
	if (entry.setupCoverage !== undefined) {
		if (!entry.setupCoverage || typeof entry.setupCoverage !== 'object') {
			throw new Error(`Capability ${entry.id} has invalid setupCoverage`);
		}
		const setup = entry.setupCoverage as Record<string, unknown>;
		if (typeof setup.newProject !== 'string' || typeof setup.existingProject !== 'string') {
			throw new Error(
				`Capability ${entry.id} must declare newProject and existingProject setup coverage`
			);
		}
		setupCoverage = { newProject: setup.newProject, existingProject: setup.existingProject };
	}
	return {
		id: entry.id as string,
		name: entry.name as string,
		category: entry.category as string,
		status: entry.status as CapabilityStatus,
		summary: entry.summary as string,
		qualificationLimits: entry.qualificationLimits as string[],
		owners: entry.owners as string[],
		evidence: entry.evidence as string[],
		adapters: entry.adapters as string[],
		guides: entry.guides as string[],
		symbols: entry.symbols as string[],
		setupCoverage
	};
}

export const capabilities: Capability[] = (registryData as unknown[])
	.map(parseCapability)
	.sort((left, right) => left.id.localeCompare(right.id));

const capabilityById = new Map(capabilities.map((capability) => [capability.id, capability]));

if (capabilityById.size !== capabilities.length) {
	throw new Error('Capability registry IDs must be unique');
}

export function getCapability(id: string): Capability | undefined {
	return capabilityById.get(id);
}

export function requireCapability(id: string): Capability {
	const capability = getCapability(id);
	if (!capability) throw new Error(`Unknown capability ID ${id}`);
	return capability;
}

export function capabilitiesFor(ids: string[]): Capability[] {
	return ids.map(requireCapability);
}

export function primaryCapabilityStatus(ids: string[]): CapabilityStatus | undefined {
	const priority: Record<CapabilityStatus, number> = {
		planned: 4,
		experimental: 3,
		limited: 2,
		available: 1
	};
	return capabilitiesFor(ids)
		.map((capability) => capability.status)
		.sort((left, right) => priority[right] - priority[left])[0];
}
