# PostgreSQL

The chart connects to an external PostgreSQL server, which must be reachable from the
cluster. Both OpenWebUI and Authentik default to `sslmode=require`, so the server should
accept TLS. Against one that does not, set `openwebui.postgres.sslMode` and
`authentik.authentik.postgresql.sslmode` to `disable`, which leaves all database traffic
in cleartext for as long as the deployment runs.

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

> [!IMPORTANT]
>
> The sql script sends the application passwords in `CREATE ROLE` statements, so connect
> over TLS. The recipe defaults to `sslmode=require`. When running manually, use:
>
> ```bash
> psql "postgresql://<admin>@<host>/postgres?sslmode=require" -f tools/scripts/bootstrap-db.sql
> ```
>
> Without TLS, `PGSSLMODE=disable just db-bootstrap <host>` sends those passwords in
> cleartext; run it from a pod inside the cluster.

## Values

The postgres values must be specified for `authentik` and `openwebui`. They may use the same
server, but should have different roles and databases.

## Passwords

The openwebui postgres password is read from Kubernetes `Secret` resources.
Simply specify the resource name:

```yaml
openwebui:
  postgres:
    passwordSecret:
      name: vllm-openwebui-pg
```

For the Authentik subchart to also use an external secret, the password must be injected via `global.env`, which covers server and worker at
once:

```yaml
authentik:
  authentik:
    postgresql:
      password: # unset on purpose, supplied below
  global:
    env:
      - name: AUTHENTIK_POSTGRESQL__PASSWORD
        valueFrom:
          secretKeyRef: { name: vllm-authentik-pg, key: password }
```

> [!IMPORTANT]
>
> Use alphanumeric passwords. OpenWebUI's is interpolated into a connection URI in the pod,
> where nothing percent-encodes it, so `@ : / ? # %` would corrupt the DSN.

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
