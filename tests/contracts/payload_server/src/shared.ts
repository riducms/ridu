import type { Access, FieldAccess } from "payload";

export const roles = {
	administrator: "administrator",
	contributor: "contributor",
	editor: "editor",
} as const;

type Role = (typeof roles)[keyof typeof roles];

type FixtureUser = {
	collection?: string;
	id: number | string;
	role?: Role;
};

const isFixtureUser = (user: unknown): user is FixtureUser => {
	return Boolean(user && typeof user === "object" && "id" in user);
};

export const hasRole = (user: unknown, ...allowedRoles: Role[]): boolean => {
	return isFixtureUser(user) && Boolean(user.role && allowedRoles.includes(user.role));
};

export const allowEveryone: Access = () => true;

export const allowAuthenticated: Access = ({ req }) => Boolean(req.user);

export const allowRoles = (...allowedRoles: Role[]): Access => {
	return ({ req }) => hasRole(req.user, ...allowedRoles);
};

export const allowSelfOrRoles = (...allowedRoles: Role[]): Access => {
	return ({ id, req }) => {
		return (
			hasRole(req.user, ...allowedRoles) || Boolean(isFixtureUser(req.user) && id === req.user.id)
		);
	};
};

export const allowOwnedOrRoles = (ownerField: string, ...allowedRoles: Role[]): Access => {
	return ({ req }) => {
		if (hasRole(req.user, ...allowedRoles)) {
			return true;
		}

		if (!isFixtureUser(req.user)) {
			return false;
		}

		return {
			[ownerField]: {
				equals: req.user.id,
			},
		};
	};
};

export const allowFieldRoles = (...allowedRoles: Role[]): FieldAccess => {
	return ({ req }) => hasRole(req.user, ...allowedRoles);
};

export const staffManagedPublicRead = {
	create: allowRoles(roles.administrator, roles.editor),
	delete: allowRoles(roles.administrator),
	read: allowEveryone,
	update: allowRoles(roles.administrator, roles.editor),
};

export const slugs = {
	categories: "categories",
	editorialNotes: "editorial-notes",
	events: "events",
	media: "media",
	pages: "pages",
	payloadCapabilities: "payload-capabilities",
	posts: "posts",
	redirects: "redirects",
	users: "users",
} as const;
