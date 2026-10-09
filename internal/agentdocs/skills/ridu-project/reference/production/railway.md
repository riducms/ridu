<!-- Generated from website/src/content/docs/production/railway.md by scripts/sync-agent-docs.ts. -->

# Deploy to Railway

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
the project was created:

```text title="Railway service settings"
Build command:      bun run build
Pre-deploy command: bun run migrate
Start command:      bun run start
Healthcheck path:   /readyz
```

The build command produces `dist/<project>` with the embedded admin. The pre-deploy command applies
committed migrations before the release starts, and `/readyz` keeps traffic away until the database
passes readiness.

The pre-deploy command has access to service variables and runs before the new application starts.
Give it a timeout that is longer than a rehearsed migration but still fails a stuck deployment.
See Railway's [pre-deploy command guide](https://docs.railway.com/deployments/pre-deploy-command)
for where to set the command and timeout.

## Set production variables {#variables}

Add these variables after Railway creates the public domain:

```dotenv title="Railway variables"
RIDU_ALLOWED_HOSTS=${{RAILWAY_PUBLIC_DOMAIN}},healthcheck.railway.app
RIDU_READINESS_DRAIN_DELAY=-1s
```

Ridu refuses requests for any host `RIDU_ALLOWED_HOSTS` doesn't list. Railway sends its healthcheck
with the host `healthcheck.railway.app`, so keep that host in the list: without it every
healthcheck is refused and the deployment never goes live. Ridu logs the first refusal of each
host. `${{RAILWAY_PUBLIC_DOMAIN}}` follows the service's generated domain if it's renamed. Add a
custom domain to the list as well.

`DATABASE_URL` comes from the PostgreSQL service. Leave `RIDU_TRUSTED_PROXY_CIDRS` empty: Railway's
edge terminates HTTPS and forwards plain HTTP from addresses it doesn't publish. Ridu still accepts
the HTTPS origin of the domain each request names, so the admin signs in without
`RIDU_ALLOWED_ORIGINS`. Add that variable only when a browser application on another origin calls
the API.

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
bun run migrate && exec bun run start
```

Keep the replica count at one. A Railway volume is attached to one service replica, and Ridu's
SQLite support is limited to one application host. Use PostgreSQL before adding replicas.
