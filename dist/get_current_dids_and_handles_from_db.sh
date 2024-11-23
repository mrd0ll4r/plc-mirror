#!/bin/bash -e

#-- This first filters to only include the latest DID document state.
#-- We then extract DID and handle.
#-- It allocates a giant temporary table, so it's pretty slow and resource-hungry.
QUERY="SELECT t.did, t.operation->'alsoKnownAs'->0 AS aka, t.operation->'handle' AS handle FROM (SELECT *, ROW_NUMBER() OVER (PARTITION BY did ORDER BY plc_timestamp DESC) as r FROM plc_log_entries WHERE nullified=FALSE) AS t WHERE t.r=1 AND t.operation->'type'->>0 != 'plc_tombstone'::TEXT;"

docker compose exec -u postgres db psql bluesky_plc --csv -c "$QUERY" > accounts.csv
