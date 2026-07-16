#!/bin/bash
set -e

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" <<-EOSQL
    CREATE DATABASE printerops;
    GRANT ALL PRIVILEGES ON DATABASE printerops TO $POSTGRES_USER;
EOSQL
