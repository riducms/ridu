import type { APIRoute } from 'astro';
import { buildSiteSearchIndex } from '@/search';

export const prerender = true;

export const GET: APIRoute = async () =>
	new Response(JSON.stringify(await buildSiteSearchIndex()), {
		headers: {
			'Content-Type': 'application/json; charset=utf-8',
			'Cache-Control': 'public, max-age=0, must-revalidate'
		}
	});
