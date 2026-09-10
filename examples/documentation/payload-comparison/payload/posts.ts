import type { Access, CollectionConfig } from 'payload';

const signedIn: Access = ({ req: { user } }) => Boolean(user);

export const Posts: CollectionConfig = {
	slug: 'posts',
	// Match Ridu's default: document locking is off.
	lockDocuments: false,
	admin: {
		useAsTitle: 'title',
		defaultColumns: ['title', 'author', 'updatedAt']
	},
	fields: [
		{
			name: 'title',
			type: 'text',
			required: true,
			maxLength: 120
		},
		{
			name: 'slug',
			type: 'text',
			required: true,
			unique: true,
			index: true
		},
		{ name: 'summary', type: 'textarea' },
		// Store the user's ID. Ownership permissions come later.
		{
			name: 'author',
			type: 'relationship',
			relationTo: 'users',
			required: true
		}
	],
	access: {
		read: () => true,
		create: signedIn,
		update: signedIn,
		delete: signedIn
	}
};
