# PostgreSQL

The chart connects to an external PostgreSQL server, which
must be reachable from the cluster and accept TLS: both OpenWebUI and Authentik connect with
`sslmode=require`.

## Roles and databases

Run this once per environment, before installing:

```bash
just db-bootstrap <host> [<admin-user>] [<openwebui-db>] [<authentik-db>]
```

`admin-user` defaults to `postgres`, and the two names to `openwebui` and `authentik`.
When several releases share one server, scope the names per release — see below.

The recipe reads the two application passwords from the files named in `.env` (copy
`..tpl.env` and fill in the paths), and prompts for the admin password. It needs an account
allowed to `CREATE ROLE` and `CREATE DATABASE`; on a managed or central server you may have
to ask a DBA to run [bootstrap-db.sql](../tools/scripts/bootstrap-db.sql) instead. Either way
it is safe to re-run: existing roles and databases are left untouched.

## Values

Set these in your `values.<env>.yaml`:

| Value                                     | Description                                |
| ----------------------------------------- | ------------------------------------------ |
| `postgresql.host`                         | Server address, used by OpenWebUI.         |
| `openwebui.postgres.database`             | OpenWebUI's database, and its owning role. |
| `openwebui.postgres.password`             | Password of that role.                     |
| `authentik.authentik.postgresql.host`     | The same server address.                   |
| `authentik.authentik.postgresql.name`     | Authentik's database name.                 |
| `authentik.authentik.postgresql.user`     | Authentik's role name, normally the same.  |
| `authentik.authentik.postgresql.password` | Password of that role.                     |

The address appears twice because Authentik reads its database settings from its own
subchart values, which a parent chart cannot fill in. Rendering fails with a message naming
the missing key if either host is left unset.

## Sharing a server between releases

The names default to `openwebui` and `authentik`, which collide as soon as two releases use
one server. Scope them per release, and pass the same names to `just db-bootstrap`:

```yaml
openwebui:
  postgres:
    database: staging-openwebui
authentik:
  authentik:
    postgresql:
      name: staging-authentik
      user: staging-authentik
```

A collision does not fail loudly: the bootstrap skips roles that already exist, so a second
release either fails to authenticate or, if the password was copied across, quietly shares
the first release's database. Give each release its own names, or its own server.

Authentik's remaining connection settings (`port`, `sslmode`, `name`, `user`) have defaults
in `values.yaml` and rarely need changing.
