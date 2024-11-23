SELECT t.* FROM (SELECT *, ROW_NUMBER() OVER (PARTITION BY did ORDER BY plc_timestamp DESC) as r FROM plc_log_entries WHERE nullified = FALSE) AS t WHERE t.r=1;
