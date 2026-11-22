-- Initializes isolated disposable PostgreSQL databases and least-privilege roles
-- for the Ruxbase Enterprise Stress Testing & Scalability Certification Suite.
--
-- Runs as container superuser during isolated benchmark setup only.
-- Ruxbase application instances ALWAYS connect as the restricted non-superuser
-- role `ruxbase_app` (NOSUPERUSER, NOBYPASSRLS).
--
-- Usage:
--   psql -U postgres -v ON_ERROR_STOP=1 -v app_password='...' -v wal_password='...' -f init-stress-db.sql

\set ON_ERROR_STOP on

-- 1. Restricted non-superuser application role (NOSUPERUSER, NOBYPASSRLS)
--    Matches scripts/ci-db-setup.sql contract so RLS policies are genuinely enforced
--    when Ruxbase switches to NOLOGIN roles (ruxbase_api, ruxbase_authenticated, ruxbase_anon).
SELECT format(
    'CREATE ROLE ruxbase_app LOGIN CREATEDB CREATEROLE REPLICATION NOSUPERUSER NOBYPASSRLS PASSWORD %L',
    :'app_password'
)
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'ruxbase_app')
\gexec

SELECT format(
    'ALTER ROLE ruxbase_app LOGIN CREATEDB CREATEROLE REPLICATION NOSUPERUSER NOBYPASSRLS PASSWORD %L',
    :'app_password'
)
\gexec

-- 2. Dedicated least-privilege WAL replication role (no SUPERUSER, no BYPASSRLS)
SELECT format(
    'CREATE ROLE ruxbase_wal LOGIN REPLICATION NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS PASSWORD %L',
    :'wal_password'
)
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'ruxbase_wal')
\gexec

-- 3. Disposable stress databases owned by ruxbase_app
SELECT 'CREATE DATABASE stress_single OWNER ruxbase_app'
WHERE NOT EXISTS (SELECT 1 FROM pg_database WHERE datname = 'stress_single')
\gexec

SELECT 'CREATE DATABASE stress_cluster OWNER ruxbase_app'
WHERE NOT EXISTS (SELECT 1 FROM pg_database WHERE datname = 'stress_cluster')
\gexec

SELECT 'CREATE DATABASE stress_pgc OWNER ruxbase_app'
WHERE NOT EXISTS (SELECT 1 FROM pg_database WHERE datname = 'stress_pgc')
\gexec

SELECT 'CREATE DATABASE stress_core OWNER ruxbase_app'
WHERE NOT EXISTS (SELECT 1 FROM pg_database WHERE datname = 'stress_core')
\gexec

SELECT 'CREATE DATABASE stress_endurance OWNER ruxbase_app'
WHERE NOT EXISTS (SELECT 1 FROM pg_database WHERE datname = 'stress_endurance')
\gexec

-- 4. Install required extensions in each disposable database (superuser required for pgvector)
\connect stress_single
CREATE EXTENSION IF NOT EXISTS vector;
CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;

\connect stress_cluster
CREATE EXTENSION IF NOT EXISTS vector;
CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;

\connect stress_pgc
CREATE EXTENSION IF NOT EXISTS vector;
CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;

\connect stress_core
CREATE EXTENSION IF NOT EXISTS vector;
CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;

\connect stress_endurance
CREATE EXTENSION IF NOT EXISTS vector;
CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
