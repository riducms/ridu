---
title: 'Authentication and actor routes'
description: 'Authenticate REST clients and manage Ridu sessions, API keys, capabilities, preferences, locks, and preview tokens.'
product: data
eyebrow: 'REST API'
order: 102
navigation:
  section: 'Work with data'
  parent: 'rest-api'
  order: 20
  title: 'Authentication and actor routes'
---

Auth routes exist for collections with `Auth` configured. Individual feature flags decide whether
password recovery, email verification, account unlocking, and API-key routes are generated.

## Authenticate a client {#credentials}

| Client situation                                         | Credential to send                                                                      |
| -------------------------------------------------------- | --------------------------------------------------------------------------------------- |
| Browser signed in through Ridu                           | The HttpOnly `ridu_session` cookie; use `credentials: 'include'` when crossing origins. |
| Server or non-browser client with a session token        | `Authorization: Session <token>`.                                                       |
| Client with an API key                                   | `Authorization: Bearer <key>`.                                                          |
| Compatibility client with an existing Ridu session token | `Authorization: JWT <token>` remains accepted.                                          |

Login establishes a SameSite=Lax cookie. API-key creation specifically requires cookie
authentication, and the returned secret is shown once; a later list call only returns metadata.

## Create users and manage sign-in {#accounts}

| Method and path                                    | What it does                                                |
| -------------------------------------------------- | ----------------------------------------------------------- |
| `GET /api/auth/{collection}/bootstrap`             | Check whether one-time first-admin setup is available.      |
| `POST /api/auth/{collection}/create-user`          | Create a user with `{ "data": { … }, "password": "…" }`.    |
| `POST /api/auth/{collection}/login`                | Authenticate with email and password.                       |
| `POST /api/auth/{collection}/forgot-password`      | Request a recovery notification when enabled.               |
| `POST /api/auth/{collection}/reset-password`       | Consume a recovery token when enabled.                      |
| `POST /api/auth/{collection}/request-verification` | Request an email-verification notification when enabled.    |
| `POST /api/auth/{collection}/verify`               | Consume an email-verification token when enabled.           |
| `POST /api/auth/{collection}/{id}/unlock`          | Clear login-attempt lock state when enabled and authorized. |

See [Authentication](/docs/authentication/) for password policies, token delivery callbacks,
strategy authentication, lockouts, and the bootstrap lifecycle.

## Manage the current actor {#actor}

| Method and path                  | What it does                                               |
| -------------------------------- | ---------------------------------------------------------- |
| `GET /api/auth/me`               | Read the current session identity.                         |
| `POST /api/auth/refresh`         | Rotate or refresh the current session.                     |
| `POST /api/auth/logout`          | Revoke the current session.                                |
| `POST /api/auth/logout-all`      | Revoke all sessions for the actor.                         |
| `POST /api/auth/change-password` | Change the password and revoke the current cookie session. |
| `GET /api/auth/sessions`         | List sessions for the current actor.                       |
| `DELETE /api/auth/sessions/{id}` | Revoke one session.                                        |
| `GET /api/auth/api-keys`         | List API-key metadata when enabled.                        |
| `POST /api/auth/api-keys`        | Create and return a new API-key secret when enabled.       |
| `DELETE /api/auth/api-keys/{id}` | Revoke an API key.                                         |

## Ask what the actor can do {#capabilities}

Capability responses help clients choose which controls to show. Every later operation re-runs its
access rules and store predicate, so a cached capability response is never authorization.

| Method and path                                       | What it resolves                                                     |
| ----------------------------------------------------- | -------------------------------------------------------------------- |
| `POST /api/access/collections/{collection}`           | Operation and field capabilities for optional `{ id, data, trash }`. |
| `POST /api/access/collections/{collection}/selection` | An access-checked `{ where?, trash? }` to at most 100 document IDs.  |
| `POST /api/access/globals/{global}`                   | Global capabilities for optional `{ data }`.                         |

## Store preferences and coordinate editors {#preferences-locks}

| Method and path                                  | What it does                                         |
| ------------------------------------------------ | ---------------------------------------------------- |
| `GET /api/preferences/{key}`                     | Read one actor preference.                           |
| `PUT /api/preferences/{key}`                     | Store `{ "value": … }` for the actor.                |
| `DELETE /api/preferences/{key}`                  | Delete one actor preference.                         |
| `DELETE /api/preferences`                        | Reset all preferences for the actor.                 |
| `GET /api/collections/{collection}/{id}/lock`    | Inspect the current document lock.                   |
| `POST /api/collections/{collection}/{id}/lock`   | Acquire a lock with optional `{ "takeover": true }`. |
| `DELETE /api/collections/{collection}/{id}/lock` | Release the actor’s lock.                            |

See [Document locks](/docs/document-locks/) for refresh, expiry, takeover, and optimistic concurrency.

## Create a bounded preview credential {#preview}

| Method and path                                         | What it does                                                  |
| ------------------------------------------------------- | ------------------------------------------------------------- |
| `POST /api/preview/collections/{collection}/{id}/token` | Mint a short-lived collection preview token.                  |
| `GET /api/preview/collections/{collection}/{id}`        | Read preview content with that token as Bearer authorization. |
| `POST /api/preview/globals/{global}/token`              | Mint a short-lived global preview token.                      |
| `GET /api/preview/globals/{global}`                     | Read a global preview with that token.                        |
| `POST /api/preview/token/revoke`                        | Revoke `{ "token": "…" }`.                                    |

Preview tokens are bound to one resource and are not general API keys. See [Live preview](/guides/live-preview/)
for the iframe URL and update channel.
