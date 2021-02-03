INSERT INTO accounts (name, auth_tenant_id, upstream_peer_hostname, required_labels, metadata_json, next_blob_sweep_at, next_storage_sweep_at, next_federation_announcement_at, in_maintenance, external_peer_url, external_peer_username, external_peer_password, platform_filter) VALUES ('test1', 'test1authtenant', '', '', '', NULL, NULL, NULL, FALSE, '', '', '', '');

INSERT INTO manifests (repo_id, digest, media_type, size_bytes, pushed_at, validated_at, validation_error_message, last_pulled_at, next_vuln_check_at, vuln_status, vuln_scan_error) VALUES (1, 'sha256:8a9217f1887083297faf37cb2c1808f71289f0cd722d6e5157a07be1c362945f', 'application/vnd.docker.distribution.manifest.v2+json', 1367, 3600, 133200, 'manifest blob unknown to registry: sha256:712dfd307e9f735a037e1391f16c8747e7fb0d1318851e32591b51a6bc600c2d', NULL, NULL, 'Pending', '');

INSERT INTO repos (id, account_name, name, next_blob_mount_sweep_at, next_manifest_sync_at) VALUES (1, 'test1', 'foo', NULL, NULL);
