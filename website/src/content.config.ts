import { defineCollection } from 'astro:content';
import { glob } from 'astro/loaders';
import { z } from 'astro/zod';

const documentationSection = z.enum([
	'Get started',
	'Model content',
	'Work with data',
	'Admin & workflows',
	'Extend Ridu',
	'Develop & operate'
]);

const hierarchicalNavigation = z.object({
	section: documentationSection,
	parent: z.string().min(1).optional(),
	group: z.string().min(1).optional(),
	order: z.number().int().nonnegative(),
	title: z.string().min(1)
});

const documentationSchema = z.object({
	title: z.string().min(1),
	description: z.string().min(1),
	product: z
		.enum(['core', 'data', 'admin', 'sdk', 'cli', 'adapters', 'plugins', 'guides'])
		.optional(),
	eyebrow: z.string().min(1).optional(),
	order: z.number().int().nonnegative().optional(),
	aliases: z.array(z.string().min(1)).default([]),
	capabilities: z.array(z.string().min(1)).default([]),
	capabilityIds: z.array(z.string().min(1)).default([]),
	symbols: z.array(z.string().min(1)).default([]),
	relatedSymbolIds: z.array(z.string().min(1)).default([]),
	layout: z.enum(['standard', 'wide']).default('standard'),
	availability: z
		.object({
			status: z.enum(['available', 'limited', 'experimental', 'planned']),
			label: z.string().min(1),
			description: z.string().min(1),
			anchor: z.string().min(1).optional()
		})
		.optional(),
	navigation: hierarchicalNavigation
});

const docs = defineCollection({
	loader: glob({ pattern: '**/*.md', base: './src/content/docs' }),
	schema: documentationSchema
});

const guides = defineCollection({
	loader: glob({ pattern: '**/*.md', base: './src/content/guides' }),
	schema: documentationSchema
});

export const collections = { docs, guides };
