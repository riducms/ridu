// Code generated shape used to compile the public Quickstart SDK example.
// A real project receives this file from `ridu generate`.

import { createClient as createRuntimeClient } from '@riducms/sdk';
import type { ClientOptions, RiduClient } from '@riducms/sdk';

export interface ScalarWhere<Value> {
	equals?: Value;
	notEquals?: Value;
	in?: readonly Value[];
	exists?: boolean;
}

export interface Users {
	id: string;
	createdAt: string;
	updatedAt: string;
	email: string;
}

export interface Posts {
	id: string;
	createdAt: string;
	updatedAt: string;
	title: string;
	summary: string | null;
	status: 'draft' | 'published' | null;
	author: string | Users | null;
}

export interface RiduConfig {
	collections: {
		users: {
			auth: true;
			upload: false;
			versions: false;
			trash: false;
			output: Users;
			create: { email: string };
			update: { email?: string };
			where: { email?: ScalarWhere<string> };
			select: { id?: boolean; email?: boolean };
			populate: Record<never, never>;
		};
		posts: {
			auth: false;
			upload: false;
			versions: false;
			trash: false;
			output: Posts;
			create: {
				title: string;
				summary?: string | null;
				status?: 'draft' | 'published' | null;
				author?: string | null;
			};
			update: {
				title?: string;
				summary?: string | null;
				status?: 'draft' | 'published' | null;
				author?: string | null;
			};
			where: {
				title?: ScalarWhere<string>;
				summary?: ScalarWhere<string>;
				status?: ScalarWhere<'draft' | 'published'>;
				author?: ScalarWhere<string>;
			};
			select: {
				id?: boolean;
				createdAt?: boolean;
				title?: boolean;
				summary?: boolean;
				status?: boolean;
				author?: boolean;
			};
			populate: { author?: boolean };
		};
	};
}

declare module '@riducms/sdk' {
	interface GeneratedRiduConfigRegistry {
		'documentation-quickstart': RiduConfig;
	}
}

export function createClient(options: ClientOptions): RiduClient<RiduConfig> {
	return createRuntimeClient<RiduConfig>(options);
}
