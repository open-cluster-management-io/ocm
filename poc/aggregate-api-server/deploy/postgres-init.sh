#!/usr/bin/env bash
set -euo pipefail

psql --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" \
  --set=app_password="$OCM_API_POC_DB_PASSWORD" <<'SQL'
CREATE ROLE manifestwork_poc LOGIN PASSWORD :'app_password';
CREATE DATABASE manifestwork_poc OWNER manifestwork_poc;
SQL
