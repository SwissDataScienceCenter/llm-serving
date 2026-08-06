-- Create the roles and databases the chart expects. Idempotent.
--
-- \gexec executes the statement the SELECT returns, and only when it returns a row.
-- CREATE DATABASE cannot run inside a DO block, so roles and databases both use this pattern rather than one each.
-- %I quotes identifiers, so scoped names containing hyphens or capitals are safe.
-- Each database is owned by a role of the same name.

\getenv openwebui_db       OPENWEBUI_PG_DATABASE
\getenv openwebui_password OPENWEBUI_PG_PASSWORD
\getenv authentik_db       AUTHENTIK_PG_DATABASE
\getenv authentik_password AUTHENTIK_PG_PASSWORD

SELECT format('CREATE ROLE %I LOGIN PASSWORD %L', :'openwebui_db', :'openwebui_password')
 WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname = :'openwebui_db')\gexec

SELECT format('CREATE DATABASE %I OWNER %I', :'openwebui_db', :'openwebui_db')
 WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = :'openwebui_db')\gexec

SELECT format('CREATE ROLE %I LOGIN PASSWORD %L', :'authentik_db', :'authentik_password')
 WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname = :'authentik_db')\gexec

SELECT format('CREATE DATABASE %I OWNER %I', :'authentik_db', :'authentik_db')
 WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = :'authentik_db')\gexec
