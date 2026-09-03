export const packageManagers = [
	{
		id: 'npm',
		label: 'npm'
	},
	{
		id: 'bun',
		label: 'Bun'
	},
	{
		id: 'pnpm',
		label: 'pnpm'
	},
	{
		id: 'yarn',
		label: 'Yarn'
	}
] as const;

export type PackageManager = (typeof packageManagers)[number]['id'];

const packageManagerIDs = new Set<string>(packageManagers.map((manager) => manager.id));

export function isPackageManager(value: string | null | undefined): value is PackageManager {
	return Boolean(value && packageManagerIDs.has(value));
}
