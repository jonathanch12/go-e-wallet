-- Creates the two application databases on first container start.
-- The default POSTGRES_DB (set in compose) creates one; this script adds the
-- rest so both services have their own database on a single Postgres instance.

CREATE DATABASE ewallet_core;
CREATE DATABASE ewallet_payment;
