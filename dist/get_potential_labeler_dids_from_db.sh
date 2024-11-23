#!/bin/bash -e

QUERY=$(cat dist/get_labeler_dids_simple.sql)

docker compose exec -u postgres db psql bluesky_plc --csv -t -c "$QUERY" > labeler_dids.txt
