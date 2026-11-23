-- Provision strictly separated least-privilege PostgreSQL roles for W11-05 Staging Soak.
-- Executed ONCE during staging bootstrap as the container superuser (postgres).
-- Running application services MUST NEVER connect as postgres.
--
-- Usage:
--   psql -U postgres -v ON_ERROR_STOP=1 \
--     -v app_password="$RUXBASE_APP_PASSWORD" \
--     -v migrator_password="$RUXBASE_MIGRATOR_PASSWORD" \
--     -v wal_password="$RUXBASE_WAL_PASSWORD" \
--     -v monitor_password="$RUXBASE_MONITOR_PASSWORD" \
--     -f init-staging-roles.sql

\set ON_ERROR_STOP on

-- 1. Restricted Runtime Application Role (NOSUPERUSER, NOBYPASSRLS, NOCREATEDB, NOCREATEROLE)
--    NOTE: Ruxbase creates collections and manages RLS roles (ruxbase_api/ruxbase_authenticated/ruxbase_anon)
--    during initial setup. When pre-provisioned migrations are run via ruxbase_migrator, ruxbase_app
--    remains strictly NOSUPERUSER and NOBYPASSRLS at all times.
SELECT format(
    'CREATE ROLE ruxbase_app LOGIN CREATEROLE REPLICATION NOSUPERUSER NOCREATEDB NOBYPASSRLS PASSWORD %L',
    :'app_password'
)
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'ruxbase_app')
\gexec

SELECT format(
    'ALTER ROLE ruxbase_app LOGIN CREATEROLE REPLICATION NOSUPERUSER NOCREATEDB NOBYPASSRLS PASSWORD %L',
    :'app_password'
)
\gexec

-- 2. Dedicated Schema Migrator Role (NOSUPERUSER, NOBYPASSRLS)
SELECT format(
    'CREATE ROLE ruxbase_migrator LOGIN CREATEROLE NOSUPERUSER NOCREATEDB NOBYPASSRLS PASSWORD %L',
    :'migrator_password'
)
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'ruxbase_migrator')
\gexec

SELECT format(
    'ALTER ROLE ruxbase_migrator LOGIN CREATEROLE NOSUPERUSER NOCREATEDB NOBYPASSRLS PASSWORD %L',
    :'migrator_password'
)
\gexec

-- 3. Dedicated Logical Replication / WAL Reader Role (REPLICATION only; NOSUPERUSER, NOBYPASSRLS)
SELECT format(
    'CREATE ROLE ruxbase_wal LOGIN REPLICATION NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS PASSWORD %L',
    :'wal_password'
)
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'ruxbase_wal')
\gexec

SELECT format(
    'ALTER ROLE ruxbase_wal LOGIN REPLICATION NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS PASSWORD %L',
    :'wal_password'
)
\gexec

-- 4. Dedicated Read-Only Monitoring Role (pg_monitor; NOSUPERUSER, NOBYPASSRLS)
SELECT format(
    'CREATE ROLE ruxbase_monitor LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS PASSWORD %L',
    :'monitor_password'
)
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'ruxbase_monitor')
\gexec

SELECT format(
    'ALTER ROLE ruxbase_monitor LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS PASSWORD %L',
    :'monitor_password'
)
\gexec

GRANT pg_monitor TO ruxbase_monitor;

-- 5. Staging Database (owned by ruxbase_app; public CONNECT revoked)
SELECT 'CREATE DATABASE ruxbase_staging OWNER ruxbase_app'
WHERE NOT EXISTS (SELECT 1 FROM pg_database WHERE datname = 'ruxbase_staging')
\gexec

REVOKE CONNECT ON DATABASE ruxbase_staging FROM PUBLIC;
GRANT CONNECT ON DATABASE ruxbase_staging TO ruxbase_app, ruxbase_migrator, ruxbase_wal, ruxbase_monitor;

\connect ruxbase_staging

CREATE EXTENSION IF NOT EXISTS vector;
CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;

GRANT USAGE ON SCHEMA public TO ruxbase_monitor;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO ruxbase_monitor;
ALTER DEFAULT PRIVILEGES FOR ROLE ruxbase_app IN SCHEMA public GRANT SELECT ON TABLES TO ruxbase_monitor;

-- Bound idle/runaway transactions for monitoring role
ALTER ROLE ruxbase_monitor SET statement_timeout = '15s';
ALTER ROLE ruxbase_monitor SET idle_in_transaction_session_timeout = '15s';
ALTER ROLE ruxbase_monitor CONNECTION LIMIT 10;

-- Guard assertion: fail bootstrap if ruxbase_app ever holds elevated privileges
DO $$
DECLARE
    v_super boolean;
    v_bypass boolean;
BEGIN
    SELECT rolsuper, rolbypassrls INTO v_super, v_bypass
    FROM pg_roles WHERE rolname = 'ruxbase_app';
    IF v_super OR v_bypass THEN
        RAISE EXCEPTION 'SECURITY VIOLATION: ruxbase_app holds SUPERUSER=% or BYPASSRLS=%', v_super, v_bypass;
    END IF;
END $$;
