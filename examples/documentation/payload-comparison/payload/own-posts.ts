import type { Access } from 'payload';

export const ownPosts: Access = ({ req: { user } }) => {
	if (!user) return false;
	// Apply this author filter within the database operation.
	return {
		author: { equals: user.id }
	};
};
