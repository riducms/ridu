---
title: 'Deploy to Railway'
description: 'Deploy a generated Ridu project to Railway with PostgreSQL, migrations, health checks, and the correct start command.'
product: core
eyebrow: 'Production'
order: 222
navigation:
  section: 'Develop & operate'
  order: 64
  title: 'Deploy to Railway'
---

Generated projects include `build`, `migrate`, and `start` scripts, and the server automatically
uses Railway's `PORT`. A normal Railway deployment uses one Ridu service and one PostgreSQL
service.

## Prepare the repository {#prepare}

Create a release locally before connecting the repository:

```sh title="terminal"
ridu check
ridu migrate verify
ridu build
git add .
git commit -m "Prepare Ridu deployment"
```

`ridu migrate verify` needs a PostgreSQL `DATABASE_URL`. A local PostgreSQL service from the
generated `compose.yaml` is sufficient for this rehearsal.

Push the project to GitHub. The committed `migrations/` directory and generated contracts must
match the source used by Railway.

## Create the Railway services {#services}

1. Create a Railway project and add **PostgreSQL**.
2. Add a service from the GitHub repository containing the Ridu app.
3. Reference the PostgreSQL service's `DATABASE_URL` from the Ridu service.
4. Generate a public domain for the Ridu service.

Set these values in the Ridu service settings. Replace `bun` with the package manager selected when
the project was created, and `<project>` with the name in the `build` script's
`--output dist/<project>`:

```text title="Railway service settings"
Build command:      bun run build
Pre-deploy command: ./dist/<project> migrate up
Start command:      ./dist/<project>
Healthcheck path:   /readyz
```

The build command produces `dist/<project>` with the embedded admin. The pre-deploy command applies
committed migrations before the release starts, and `/readyz` keeps traffic away until the database
passes readiness.

Both commands run the built binary itself. Its `migrate up` applies the committed `migrations/`
without the Ridu CLI, which would compile the project with Go first, and the server runs with no
package manager in front of it. Railway bills the memory a service uses, and a start command such
as `bun run start` keeps Bun running beside the server. Railway's memory figure also counts the
files a container has read, which is why the SQLite start command below migrates with the binary
too.

Projects created before Ridu 0.24.0 have a `build` script of plain `ridu build`, which names the
binary after the directory Railway builds in. Add `--output dist/<project>` to it.

The pre-deploy command has access to service variables and runs before the new application starts.
Give it a timeout that is longer than a rehearsed migration but still fails a stuck deployment.
See Railway's [pre-deploy command guide](https://docs.railway.com/deployments/pre-deploy-command)
for where to set the command and timeout.

## Set production variables {#variables}

Add these variables after Railway creates the public domain:

```dotenv title="Railway variables"
RIDU_ALLOWED_HOSTS=${{RAILWAY_PUBLIC_DOMAIN}},healthcheck.railway.app
RIDU_TRUSTED_PROXY_CIDRS=100.64.0.0/10
RIDU_TRUSTED_PROXY_HOPS=2
RIDU_READINESS_DRAIN_DELAY=-1s
```

Ridu refuses requests for any host `RIDU_ALLOWED_HOSTS` doesn't list. Railway sends its healthcheck
with the host `healthcheck.railway.app`, so keep that host in the list: without it every
healthcheck is refused and the deployment never goes live. Ridu logs the first refusal of each
host. `${{RAILWAY_PUBLIC_DOMAIN}}` follows the service's generated domain if it's renamed. Add a
custom domain to the list as well.

`DATABASE_URL` comes from the PostgreSQL service.

Railway's edge terminates HTTPS, and its proxies currently connect to your service from
`100.64.0.0/10`. Railway doesn't document that range and may change it. The proxies pass
`X-Forwarded-For` as `<visitor>, <edge>`, replacing any value a visitor sends, and the edge's
addresses aren't published. `RIDU_TRUSTED_PROXY_CIDRS` trusts the proxies, and
`RIDU_TRUSTED_PROXY_HOPS=2` counts the edge in front of them, so Ridu sees each visitor's address.
Sign-in limits, 10 attempts a minute per address by default, then apply to each visitor. Without
these two variables Ridu sees the proxy's address, and one person's wrong passwords can block
everyone's sign-in until the minute ends. An attacker with many addresses gets more attempts, but
account lockout still locks an account after 5 wrong passwords from any address.

Ridu follows the HTTPS scheme the trusted proxies report, so the admin signs in without
`RIDU_ALLOWED_ORIGINS`. Add that variable only when a browser application on another origin calls
the API. If Railway's proxies move to another range, Ridu stops trusting them: sign-in still works,
and every visitor shares the proxy's limit again.

Railway supplies `PORT`; generated Ridu servers turn it into the listener address automatically.
`RIDU_ADDRESS` remains available when you need to override the complete address yourself.

## Deploy and check the result {#deploy}

Trigger the deployment, then watch these three stages in order:

1. `ridu build` ends with `Built dist/<project>`.
2. The pre-deploy command ends with `Migrations are current.`
3. `/readyz` returns a successful response and Railway sends traffic to the new release.

Open the generated domain and create the first user if the database is empty. If the service exits
before binding its port, inspect `ridu migrate status` against the same `DATABASE_URL`; startup
rejects pending, missing, reordered, or modified migration artifacts.

## Use SQLite on Railway {#sqlite}

PostgreSQL is the simpler Railway choice. If the project must use SQLite, keep exactly one Ridu
replica and mount a Railway volume at `/data`. Set:

```dotenv title="Railway variables"
RIDU_SQLITE_PATH=/data/ridu.sqlite
```

Railway does not mount volumes into pre-deploy containers, so run the SQLite migration from the
start command instead:

```sh title="Railway start command"
./dist/<project> migrate up && exec ./dist/<project>
```

Keep the replica count at one. A Railway volume is attached to one service replica, and Ridu's
SQLite support is limited to one application host. Use PostgreSQL before adding replicas.
