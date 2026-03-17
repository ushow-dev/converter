-- Migration 011: seed remote storage location
-- base_url is empty until a domain/proxy is configured.
-- Update it later: UPDATE storage_locations SET base_url='https://...' WHERE name='remote-sftp';

INSERT INTO storage_locations (name, type, base_url, is_active)
VALUES ('remote-sftp', 'sftp', '', true)
ON CONFLICT (name) DO NOTHING;
