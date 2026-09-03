import type { APIRoute } from 'astro';
import { buildLLMSFull } from '@/llms';

export const prerender = true;

export const GET: APIRoute = async () =>
	new Response(await buildLLMSFull(), {
		headers: { 'Content-Type': 'text/plain; charset=utf-8' }
	});
