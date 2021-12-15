INSERT INTO accounts (name, auth_tenant_id, upstream_peer_hostname, required_labels, metadata_json, next_blob_sweep_at, next_storage_sweep_at, next_federation_announcement_at, in_maintenance, external_peer_url, external_peer_username, external_peer_password, platform_filter, gc_policies_json) VALUES ('test1', 'test1authtenant', '', '', '', NULL, NULL, NULL, FALSE, '', '', '', '', '[]');

INSERT INTO blob_mounts (blob_id, repo_id, can_be_deleted_at) VALUES (1, 1, NULL);
INSERT INTO blob_mounts (blob_id, repo_id, can_be_deleted_at) VALUES (2, 1, NULL);
INSERT INTO blob_mounts (blob_id, repo_id, can_be_deleted_at) VALUES (3, 1, NULL);
INSERT INTO blob_mounts (blob_id, repo_id, can_be_deleted_at) VALUES (5, 1, NULL);

INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, validated_at, validation_error_message, can_be_deleted_at, media_type) VALUES (1, 'test1', 'sha256:a718f4a112454b50c8ecd2b0a5b00eb32ee90699593625139cd3fabc97dcce8d', 1048576, '6b86b273ff34fce19d6b804eff5a3f5747ada4eaa22f1d49c01e52ddb7875b4b', 3600, 3600, '', NULL, 'application/vnd.docker.image.rootfs.diff.tar.gzip');
INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, validated_at, validation_error_message, can_be_deleted_at, media_type) VALUES (2, 'test1', 'sha256:d87b0830e55e19fd0825bebaa110ebade6de7d1fcf2ddf0fbd5762e1f809370e', 1048576, 'd4735e3a265e16eee03f59718b9b5d03019c07d8b6c51f90da3a666eec13ab35', 3600, 3600, '', NULL, 'application/vnd.docker.image.rootfs.diff.tar.gzip');
INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, validated_at, validation_error_message, can_be_deleted_at, media_type) VALUES (3, 'test1', 'sha256:8744de6601dad881db9d58714824690eff63aed8033aaf025a90b8a5c8114099', 1412, '4e07408562bedb8b60ce05c1decfe3ad16b72230967de01f640b7e4729b49fce', 3600, 3600, '', NULL, 'application/vnd.docker.container.image.v1+json');
INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, validated_at, validation_error_message, can_be_deleted_at, media_type) VALUES (4, 'test1', 'sha256:4ebfd1aafe77056e348c3ef48fa229b60390abb9c15c1023f609d88c06943eab', 1048576, '4b227777d4dd1fc61c6f884f48641d02b4d121d3fd328cb08b5531fcacdabf8a', 10800, 10800, '', NULL, '');
INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, validated_at, validation_error_message, can_be_deleted_at, media_type) VALUES (5, 'test1', 'sha256:65c59193f05fcb20c54ad7ac25585907a04553033a9a61dbd32ced96fdd24fe1', 1048576, 'ef2d127de37b942baad06145e54b0c619a1f22327b2ebbcfbec78f5564afe39d', 10800, 10800, '', NULL, '');

INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES (1, 'sha256:7ce8d2ddbc66e475563019803ff254fb78b7becafd39959dc735ace4efaf395e', 1);
INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES (1, 'sha256:7ce8d2ddbc66e475563019803ff254fb78b7becafd39959dc735ace4efaf395e', 2);
INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES (1, 'sha256:7ce8d2ddbc66e475563019803ff254fb78b7becafd39959dc735ace4efaf395e', 3);
INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES (1, 'sha256:7ce8d2ddbc66e475563019803ff254fb78b7becafd39959dc735ace4efaf395e', 5);

INSERT INTO manifest_contents (repo_id, digest, content) VALUES (1, 'sha256:7ce8d2ddbc66e475563019803ff254fb78b7becafd39959dc735ace4efaf395e', '{"config":{"digest":"sha256:8744de6601dad881db9d58714824690eff63aed8033aaf025a90b8a5c8114099","mediaType":"application/vnd.docker.container.image.v1+json","size":1412},"layers":[{"digest":"sha256:a718f4a112454b50c8ecd2b0a5b00eb32ee90699593625139cd3fabc97dcce8d","mediaType":"application/vnd.docker.image.rootfs.diff.tar.gzip","size":1048576},{"digest":"sha256:d87b0830e55e19fd0825bebaa110ebade6de7d1fcf2ddf0fbd5762e1f809370e","mediaType":"application/vnd.docker.image.rootfs.diff.tar.gzip","size":1048576}],"mediaType":"application/vnd.docker.distribution.manifest.v2+json","schemaVersion":2}');

INSERT INTO manifests (repo_id, digest, media_type, size_bytes, pushed_at, validated_at, validation_error_message, last_pulled_at, next_vuln_check_at, vuln_status, vuln_scan_error, labels_json, gc_status_json, min_layer_created_at, max_layer_created_at) VALUES (1, 'sha256:7ce8d2ddbc66e475563019803ff254fb78b7becafd39959dc735ace4efaf395e', 'application/vnd.docker.distribution.manifest.v2+json', 2099156, 3600, 3600, '', NULL, NULL, 'Pending', '', '', '', NULL, NULL);

INSERT INTO quotas (auth_tenant_id, manifests) VALUES ('test1authtenant', 100);

INSERT INTO repos (id, account_name, name, next_blob_mount_sweep_at, next_manifest_sync_at, next_gc_at) VALUES (1, 'test1', 'foo', 21600, NULL, NULL);
