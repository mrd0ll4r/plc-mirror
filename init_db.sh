#!/bin/bash
set -e

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" <<-EOSQL
    CREATE USER bluesky_plc;
    CREATE DATABASE bluesky_plc;
    ALTER DATABASE bluesky_plc OWNER TO bluesky_plc;
    ALTER USER bluesky_plc WITH ENCRYPTED PASSWORD 'bluesky_plc';
EOSQL
