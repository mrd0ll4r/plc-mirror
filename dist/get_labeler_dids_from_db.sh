#!/bin/bash -e

docker compose exec -u postgres db psql bluesky_plc --csv -c "SELECT DISTINCT did FROM plc_log_entries WHERE jsonb_path_exists(operation, '$.services.atproto_labeler');" |
    # remove header
    tail -n+2 > labeler_dids.txt
