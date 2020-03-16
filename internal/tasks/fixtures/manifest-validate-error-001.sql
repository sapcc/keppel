INSERT INTO accounts (name, auth_tenant_id, upstream_peer_hostname, required_labels, blobs_sweeped_at, storage_sweeped_at) VALUES ('test1', 'test1authtenant', '', '', NULL, NULL);

INSERT INTO manifests (repo_id, digest, media_type, size_bytes, pushed_at, validated_at, validation_error_message) VALUES (1, 'sha256:8a9217f1887083297faf37cb2c1808f71289f0cd722d6e5157a07be1c362945f', 'application/vnd.docker.distribution.manifest.v2+json', 1367, 3600, 46800, 'manifest blob unknown to registry: sha256:712dfd307e9f735a037e1391f16c8747e7fb0d1318851e32591b51a6bc600c2d');

INSERT INTO repos (id, account_name, name, blob_mounts_sweeped_at, manifests_synced_at) VALUES (1, 'test1', 'foo', NULL, NULL);
