# PostgreSQL

The chart connects to an external PostgreSQL server, which
must be reachable from the cluster and accept TLS: both OpenWebUI and Authentik connect with
`sslmode=require`.

## Roles and databases

The databases and roles need to be created ahead of time. We provide a helper just recipe to do it.

To use it, run this before installing:

```bash
just db-bootstrap <host> [<admin-user>] [<openwebui-db>] [<authentik-db>]
```

The recipe reads the two application passwords from the files named in `.env` (copy
`.tpl.env` and fill in the paths), and prompts for the admin password. It needs an account
allowed to `CREATE ROLE` and `CREATE DATABASE`; on a managed or central server you may have
to ask a DBA to run [bootstrap-db.sql](../tools/scripts/bootstrap-db.sql) instead. Either way
it is safe to re-run: existing roles and databases are left untouched.

> [!NOTE]
>
> The sql script sends the application passwords in `CREATE ROLE` statements, so connect over TLS.
> The recipe does this for you. When running manually, use :
>
> ```bash
> psql "postgresql://<admin>@<host>/postgres?sslmode=require" -f tools/scripts/bootstrap-db.sql
> ```

## Values

The postgres values must be specified for `authentik` and `openwebui`. They may use the same
server, but should have different roles and databases.

## Sharing a server between releases

The names default to `vllm-openwebui` and `vllm-authentik`, if multiple
releases share one server. Scope them per release, and pass the same names to
`just db-bootstrap`:

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
