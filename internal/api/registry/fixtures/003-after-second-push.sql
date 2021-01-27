INSERT INTO accounts (name, auth_tenant_id, upstream_peer_hostname, required_labels, metadata_json, next_blob_sweep_at, next_storage_sweep_at, next_federation_announcement_at, in_maintenance, external_peer_url, external_peer_username, external_peer_password) VALUES ('test1', 'test1authtenant', '', '', '', NULL, NULL, NULL, FALSE, '', '', '');

INSERT INTO blob_mounts (blob_id, repo_id, can_be_deleted_at) VALUES (1, 1, NULL);
INSERT INTO blob_mounts (blob_id, repo_id, can_be_deleted_at) VALUES (2, 1, NULL);

INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, validated_at, validation_error_message, can_be_deleted_at) VALUES (1, 'test1', 'sha256:7575de20fdeacfb9a529c26f03c5beab401bb985721b923bba6b34fe4fce5f9c', 1486, '6b86b273ff34fce19d6b804eff5a3f5747ada4eaa22f1d49c01e52ddb7875b4b', 3, 3, '', NULL);
INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, validated_at, validation_error_message, can_be_deleted_at) VALUES (2, 'test1', 'sha256:27329396f45bf916fc3c4af52592ca88235f34bb7b4475bc52ed452058d8d160', 1518, 'd4735e3a265e16eee03f59718b9b5d03019c07d8b6c51f90da3a666eec13ab35', 4, 4, '', NULL);

INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES (1, 'sha256:65147aad93781ff7377b8fb81dab153bd58ffe05b5dc00b67b3035fa9420d2de', 2);
INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES (1, 'sha256:86fa8722ca7f27e97e1bc5060c3f6720bf43840f143f813fcbe48ed4cbeebb90', 1);

INSERT INTO manifests (repo_id, digest, media_type, size_bytes, pushed_at, validated_at, validation_error_message, last_pulled_at, next_vuln_check_at, vuln_status) VALUES (1, 'sha256:65147aad93781ff7377b8fb81dab153bd58ffe05b5dc00b67b3035fa9420d2de', 'application/vnd.docker.distribution.manifest.v2+json', 1783, 4, 4, '', NULL, NULL, 'Unknown');
INSERT INTO manifests (repo_id, digest, media_type, size_bytes, pushed_at, validated_at, validation_error_message, last_pulled_at, next_vuln_check_at, vuln_status) VALUES (1, 'sha256:86fa8722ca7f27e97e1bc5060c3f6720bf43840f143f813fcbe48ed4cbeebb90', 'application/vnd.docker.distribution.manifest.v2+json', 1751, 3, 3, '', NULL, NULL, 'Unknown');

INSERT INTO quotas (auth_tenant_id, manifests) VALUES ('test1authtenant', 100);

INSERT INTO repos (id, account_name, name, next_blob_mount_sweep_at, next_manifest_sync_at) VALUES (1, 'test1', 'foo', NULL, NULL);

INSERT INTO tags (repo_id, name, digest, pushed_at, last_pulled_at) VALUES (1, 'latest', 'sha256:65147aad93781ff7377b8fb81dab153bd58ffe05b5dc00b67b3035fa9420d2de', 4, NULL);
