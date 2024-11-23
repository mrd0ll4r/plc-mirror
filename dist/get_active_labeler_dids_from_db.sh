#!/bin/bash -e

#-- This first filters to only include the latest DID document state.
#-- That way, we exclude accounts that were labelers at some point but stopped since.
#-- It allocates a giant temporary table, so it's pretty slow and resource-hungry.
#QUERY="SELECT t.did FROM (SELECT *, ROW_NUMBER() OVER (PARTITION BY did ORDER BY plc_timestamp DESC) as r FROM plc_log_entries WHERE nullified=FALSE) AS t WHERE t.r=1 AND jsonb_path_exists(t.operation, '$.services.atproto_labeler');"

QUERY=$(cat dist/get_labeler_dids_active.sql)

docker compose exec -u postgres db psql bluesky_plc --csv -t -c "$QUERY" > active_labeler_dids.txt
