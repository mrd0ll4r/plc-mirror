SELECT DISTINCT did FROM plc_log_entries WHERE jsonb_path_exists(operation, '$.services.atproto_labeler');
