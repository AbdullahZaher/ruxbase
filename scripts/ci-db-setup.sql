-- Prepares a fresh Postgres server for the ruxbase test suite.
--
-- Run it as the container superuser. It is idempotent:
--
--   psql -v ON_ERROR_STOP=1 -v app_password="$APP_PASSWORD" -f scripts/ci-db-setup.sql
--
-- It creates the non-superuser role ruxbase_app, the databases ruxbase and
-- ruxbase_test owned by it, and the vector and pgcrypto extensions in both
-- (vector is not a trusted extension, so a superuser must create it).

\set ON_ERROR_STOP on

SELECT format(
    'CREATE ROLE ruxbase_app LOGIN CREATEDB CREATEROLE REPLICATION NOSUPERUSER NOBYPASSRLS PASSWORD %L',
    :'app_password'
)
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'ruxbase_app')
\gexec

-- keep the attributes and the password in sync when the role already exists
SELECT format(
    'ALTER ROLE ruxbase_app LOGIN CREATEDB CREATEROLE REPLICATION NOSUPERUSER NOBYPASSRLS PASSWORD %L',
    :'app_password'
)
\gexec

SELECT 'CREATE DATABASE ruxbase OWNER ruxbase_app'
WHERE NOT EXISTS (SELECT 1 FROM pg_database WHERE datname = 'ruxbase')
\gexec

SELECT 'CREATE DATABASE ruxbase_test OWNER ruxbase_app'
WHERE NOT EXISTS (SELECT 1 FROM pg_database WHERE datname = 'ruxbase_test')
\gexec

\connect ruxbase
CREATE EXTENSION IF NOT EXISTS vector;
CREATE EXTENSION IF NOT EXISTS pgcrypto;

\connect ruxbase_test
CREATE EXTENSION IF NOT EXISTS vector;
CREATE EXTENSION IF NOT EXISTS pgcrypto;
