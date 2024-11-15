#!/bin/bash -e

now=$(date -u '+%Y-%m-%d_%H-%M-%S')
outfile="plc_directory_${now}_UTC.sql.gz"

echo "It's now $now, dumping database into $outfile..."

docker compose exec -u postgres db pg_dump -C --format=plain --clean bluesky_plc |
    gzip -9 > "$outfile"
