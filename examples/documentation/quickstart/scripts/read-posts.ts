import { createClient } from '../generated/ridu.generated';

const baseURL = process.env.RIDU_URL ?? 'http://localhost:8080';
const email = process.env.RIDU_EMAIL;
const password = process.env.RIDU_PASSWORD;

if (!email || !password) {
	throw new Error(
		'Set RIDU_EMAIL and RIDU_PASSWORD to the user created in the admin.'
	);
}

let sessionCookie = '';
const ridu = createClient({
	baseURL,
	middleware: [
		async (request, next) => {
			const headers = new Headers(request.headers);
			if (sessionCookie) headers.set('Cookie', sessionCookie);

			const response = await next(new Request(request, { headers }));
			const setCookie = response.headers.get('set-cookie');
			const match = setCookie?.match(
				/(?:^|,\s*)(ridu_session=[^;,\s]+)/
			);
			const session = match?.[1];
			if (session) sessionCookie = session;
			return response;
		}
	]
});

await ridu.login('users', { email, password });
const page = await ridu.list('posts', {
	where: { status: { equals: 'published' } },
	select: { title: true, status: true },
	sort: ['-createdAt']
});

console.log(page.docs);
