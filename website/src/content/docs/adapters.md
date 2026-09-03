---
title: 'Adapters'
description: 'Connect Ridu to databases and object-storage providers without changing CMS behavior.'
product: adapters
eyebrow: 'Adapters'
order: 170
aliases: ['database adapter', 'storage adapter', 'infrastructure']
navigation:
  section: 'Develop & operate'
  order: 10
  title: 'Adapters overview'
---

Adapters connect Ridu to infrastructure that the framework already knows how to use. A database
adapter stores documents; an object-storage adapter stores upload bytes. An application normally
selects one implementation for each required runtime port.

Plugins are different: they add CMS or admin behavior such as rich text, GraphQL, fields, hooks, or
views. Multiple plugins can coexist in `Config.Plugins`; adapters do not appear there.

| Need                                                 | Use                       | Official implementation                                                              |
| ---------------------------------------------------- | ------------------------- | ------------------------------------------------------------------------------------ |
| Store content and framework state                    | `store.Store` adapter     | [PostgreSQL](/docs/postgres/), [SQLite](/docs/sqlite/), or [MongoDB](/docs/mongodb/) |
| Store upload bytes locally                           | `storage.Backend` adapter | [Object storage](/docs/storage/#local-storage)                                       |
| Store upload bytes in S3 or an S3-compatible service | `storage.Backend` adapter | [Object storage](/docs/storage/#s3-storage)                                          |
| Add rich-text fields and their editor                | Plugin                    | [Rich text](/docs/rich-text/)                                                        |
| Add a GraphQL transport                              | Plugin                    | [GraphQL](/docs/graphql/)                                                            |

Runtime adapters are created lazily with `ridu.WithStore` and `ridu.WithUploadStorage`. This keeps
database URLs, cloud credentials, open clients, and network access out of schema generation and the
canonical manifest.

PostgreSQL is the general networked and multi-replica store. SQLite is official for a database file
on one application host and small or local workloads; shared network filesystems and multi-replica
SQLite deployments are outside its support promise. MongoDB is supported only for generated
starter and blank projects on the authenticated TLS three-member replica-set,
Linux x86-64 profile documented in its guide. The three adapters implement the same portable Store
boundary; database selection does not create a private application API.
