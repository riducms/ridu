import { memoryTokenStore } from '@riducms/sdk';
import { createClient } from '../generated/ridu.generated';

const baseURL = process.env.RIDU_URL ?? 'http://localhost:8080';
const email = process.env.RIDU_EMAIL;
const password = process.env.RIDU_PASSWORD;

if (!email || !password) {
	throw new Error(
		'Set RIDU_EMAIL and RIDU_PASSWORD to the user created in the admin.'
	);
}

// A script keeps its session token in memory and sends it on each request.
const ridu = createClient({
	baseURL,
	auth: { collection: 'users', token: memoryTokenStore() }
});

await ridu.auth.login({ email, password });
const page = await ridu.list('posts', {
	where: { status: { equals: 'published' } },
	select: { title: true, status: true },
	sort: ['-createdAt']
});

console.log(page.docs);
