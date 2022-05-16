INSERT INTO accounts (name, auth_tenant_id, upstream_peer_hostname, required_labels, metadata_json, next_blob_sweep_at, next_storage_sweep_at, next_federation_announcement_at, in_maintenance, external_peer_url, external_peer_username, external_peer_password, platform_filter, gc_policies_json) VALUES ('test1', 'tenant1', '', '', '', 200, NULL, NULL, TRUE, '', '', '', '', '[]');
INSERT INTO accounts (name, auth_tenant_id, upstream_peer_hostname, required_labels, metadata_json, next_blob_sweep_at, next_storage_sweep_at, next_federation_announcement_at, in_maintenance, external_peer_url, external_peer_username, external_peer_password, platform_filter, gc_policies_json) VALUES ('test2', 'tenant2', '', '', '', NULL, NULL, NULL, TRUE, '', '', '', '', '[]');
INSERT INTO accounts (name, auth_tenant_id, upstream_peer_hostname, required_labels, metadata_json, next_blob_sweep_at, next_storage_sweep_at, next_federation_announcement_at, in_maintenance, external_peer_url, external_peer_username, external_peer_password, platform_filter, gc_policies_json) VALUES ('test3', 'tenant3', '', '', '', NULL, NULL, NULL, TRUE, '', '', '', '', '[]');

INSERT INTO blob_mounts (blob_id, repo_id, can_be_deleted_at) VALUES (1, 1, NULL);
INSERT INTO blob_mounts (blob_id, repo_id, can_be_deleted_at) VALUES (2, 1, NULL);
INSERT INTO blob_mounts (blob_id, repo_id, can_be_deleted_at) VALUES (3, 1, NULL);

INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, validated_at, validation_error_message, can_be_deleted_at, media_type, blocks_vuln_scanning) VALUES (1, 'test1', 'sha256:a718f4a112454b50c8ecd2b0a5b00eb32ee90699593625139cd3fabc97dcce8d', 1048576, '6b86b273ff34fce19d6b804eff5a3f5747ada4eaa22f1d49c01e52ddb7875b4b', 0, 0, '', NULL, '', NULL);
INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, validated_at, validation_error_message, can_be_deleted_at, media_type, blocks_vuln_scanning) VALUES (2, 'test1', 'sha256:d87b0830e55e19fd0825bebaa110ebade6de7d1fcf2ddf0fbd5762e1f809370e', 1048576, 'd4735e3a265e16eee03f59718b9b5d03019c07d8b6c51f90da3a666eec13ab35', 1, 1, '', NULL, '', NULL);
INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, validated_at, validation_error_message, can_be_deleted_at, media_type, blocks_vuln_scanning) VALUES (3, 'test1', 'sha256:8744de6601dad881db9d58714824690eff63aed8033aaf025a90b8a5c8114099', 1412, '4e07408562bedb8b60ce05c1decfe3ad16b72230967de01f640b7e4729b49fce', 2, 2, '', NULL, '', NULL);

INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES (1, 'sha256:7ce8d2ddbc66e475563019803ff254fb78b7becafd39959dc735ace4efaf395e', 1);
INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES (1, 'sha256:7ce8d2ddbc66e475563019803ff254fb78b7becafd39959dc735ace4efaf395e', 2);
INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES (1, 'sha256:7ce8d2ddbc66e475563019803ff254fb78b7becafd39959dc735ace4efaf395e', 3);

INSERT INTO manifest_contents (repo_id, digest, content) VALUES (1, 'sha256:13493afd2a64d1a14fb5ca0d2fe312117239b935ea40dc35ca59c55931a86ab7', '{"manifests":[{"digest":"sha256:7ce8d2ddbc66e475563019803ff254fb78b7becafd39959dc735ace4efaf395e","mediaType":"application/vnd.docker.distribution.manifest.v2+json","platform":{"architecture":"amd64","os":"linux"},"size":592}],"mediaType":"application/vnd.docker.distribution.manifest.list.v2+json","schemaVersion":2}');

INSERT INTO manifest_manifest_refs (repo_id, parent_digest, child_digest) VALUES (1, 'sha256:13493afd2a64d1a14fb5ca0d2fe312117239b935ea40dc35ca59c55931a86ab7', 'sha256:7ce8d2ddbc66e475563019803ff254fb78b7becafd39959dc735ace4efaf395e');

INSERT INTO manifests (repo_id, digest, media_type, size_bytes, pushed_at, validated_at, validation_error_message, last_pulled_at, next_vuln_check_at, vuln_status, vuln_scan_error, labels_json, gc_status_json, min_layer_created_at, max_layer_created_at) VALUES (1, 'sha256:13493afd2a64d1a14fb5ca0d2fe312117239b935ea40dc35ca59c55931a86ab7', 'application/vnd.docker.distribution.manifest.list.v2+json', 909, 0, 0, '', NULL, NULL, 'Pending', '', '', '', NULL, NULL);
INSERT INTO manifests (repo_id, digest, media_type, size_bytes, pushed_at, validated_at, validation_error_message, last_pulled_at, next_vuln_check_at, vuln_status, vuln_scan_error, labels_json, gc_status_json, min_layer_created_at, max_layer_created_at) VALUES (1, 'sha256:7ce8d2ddbc66e475563019803ff254fb78b7becafd39959dc735ace4efaf395e', 'application/vnd.docker.distribution.manifest.v2+json', 592, 100, 100, '', NULL, NULL, 'Pending', '', '', '', NULL, NULL);

INSERT INTO repos (id, account_name, name, next_blob_mount_sweep_at, next_manifest_sync_at, next_gc_at) VALUES (1, 'test1', 'foo/bar', NULL, NULL, NULL);
INSERT INTO repos (id, account_name, name, next_blob_mount_sweep_at, next_manifest_sync_at, next_gc_at) VALUES (2, 'test1', 'something-else', NULL, NULL, NULL);
