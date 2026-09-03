import type { APIRoute } from 'astro';
import { buildLLMSIndex } from '@/llms';

export const prerender = true;

export const GET: APIRoute = async () =>
	new Response(await buildLLMSIndex(), {
		headers: { 'Content-Type': 'text/plain; charset=utf-8' }
	});
