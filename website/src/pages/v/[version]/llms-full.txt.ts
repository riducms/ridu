import type { APIRoute } from 'astro';
import { buildLLMSFull, frameworkVersion } from '@/llms';

export const prerender = true;

export function getStaticPaths() {
	return [{ params: { version: frameworkVersion } }];
}

export const GET: APIRoute = async ({ params }) => {
	if (params.version !== frameworkVersion) return new Response('Not found', { status: 404 });
	return new Response(await buildLLMSFull(), {
		headers: { 'Content-Type': 'text/plain; charset=utf-8' }
	});
};
