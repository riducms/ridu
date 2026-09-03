import type { APIRoute } from 'astro';
import { buildLLMSIndex, frameworkVersion } from '@/llms';

export const prerender = true;

export function getStaticPaths() {
	return [{ params: { version: frameworkVersion } }];
}

export const GET: APIRoute = async ({ params }) => {
	if (params.version !== frameworkVersion) return new Response('Not found', { status: 404 });
	return new Response(await buildLLMSIndex(), {
		headers: { 'Content-Type': 'text/plain; charset=utf-8' }
	});
};
