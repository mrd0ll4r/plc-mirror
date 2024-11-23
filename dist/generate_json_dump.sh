#!/bin/bash -e

#-- This first filters to only include the latest DID document state.
#-- We then extract DID and handle.
#-- It allocates a giant temporary table, so it's pretty slow and resource-hungry.
QUERY="COPY (SELECT t.did, t.cid, t.plc_timestamp, t.operation AS newest_op FROM (SELECT *, ROW_NUMBER() OVER (PARTITION BY did ORDER BY plc_timestamp DESC) as r FROM plc_log_entries WHERE nullified=FALSE) AS t WHERE t.r=1) TO STDOUT WITH CSV HEADER;"

docker compose exec -u postgres db psql bluesky_plc -c "$QUERY" | mlr --icsv --ojsonl cat | jq -c '(.newest_op = (.newest_op | fromjson))' | gzip -9 > "plc_mirror_dump_$(date -u +'%Y-%m-%d_%H-%M-%S')_UTC.json.gz"
