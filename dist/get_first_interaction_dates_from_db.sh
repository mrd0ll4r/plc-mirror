#!/bin/bash -e

QUERY="SELECT t.did, MIN(t.plc_timestamp) AS first_op_timestamp FROM plc_log_entries t GROUP BY t.did;"

docker compose exec -u postgres db psql bluesky_plc --csv -c "$QUERY" > plc_first_interactions.csv
