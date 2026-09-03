/// <reference types="bun-types" />

import { expect, test } from 'bun:test';
import { extractMarkdownSearchSections } from './markdown';

test('indexes prose, tables, commands, code identifiers, and H2–H4 context', () => {
	const sections = extractMarkdownSearchSections(
		`Intro prose only here.

## Add a database {#existing-project}

Set DATABASE_URL before startup.

| Command | Purpose |
| --- | --- |
| ridu migrate verify | Check the history |

### Wire the adapter

\`\`\`go
store, err := mongodb.Open(ctx, os.Getenv("DATABASE_URL"))
client.LocalAPI.Find(ctx, "posts", id)
\`\`\`

#### Confirm startup

Open /readyz after ridu generate.
`,
		'MongoDB'
	);

	expect(sections.map((section) => section.heading)).toEqual([
		'MongoDB',
		'Add a database',
		'Wire the adapter',
		'Confirm startup'
	]);
	expect(sections[1].id).toBe('existing-project');
	expect(sections[1].text).toContain('DATABASE_URL');
	expect(sections[1].text).toContain('ridu migrate verify');
	expect(sections[2].identifiers).toContain('LocalAPI.Find');
	expect(sections[2].context).toEqual(['MongoDB', 'Add a database', 'Wire the adapter']);
	expect(sections[3].text).toContain('ridu generate');
});
