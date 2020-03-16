INSERT INTO accounts (name, auth_tenant_id, upstream_peer_hostname, required_labels, blobs_sweeped_at, storage_sweeped_at) VALUES ('test1', 'test1authtenant', '', '', NULL, NULL);

INSERT INTO blob_mounts (blob_id, repo_id, marked_for_deletion_at) VALUES (1, 1, NULL);
INSERT INTO blob_mounts (blob_id, repo_id, marked_for_deletion_at) VALUES (1, 2, NULL);

INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, validated_at, validation_error_message, marked_for_deletion_at) VALUES (1, 'test1', 'sha256:712dfd307e9f735a037e1391f16c8747e7fb0d1318851e32591b51a6bc600c2d', 1102, '4e07408562bedb8b60ce05c1decfe3ad16b72230967de01f640b7e4729b49fce', 1, 1, '', NULL);

INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES (1, 'sha256:8a9217f1887083297faf37cb2c1808f71289f0cd722d6e5157a07be1c362945f', 1);

INSERT INTO manifests (repo_id, digest, media_type, size_bytes, pushed_at, validated_at, validation_error_message) VALUES (1, 'sha256:8a9217f1887083297faf37cb2c1808f71289f0cd722d6e5157a07be1c362945f', 'application/vnd.docker.distribution.manifest.v2+json', 1367, 2, 2, '');

INSERT INTO quotas (auth_tenant_id, manifests) VALUES ('test1authtenant', 100);

INSERT INTO repos (id, account_name, name, blob_mounts_sweeped_at, manifests_synced_at) VALUES (1, 'test1', 'foo', NULL, NULL);
INSERT INTO repos (id, account_name, name, blob_mounts_sweeped_at, manifests_synced_at) VALUES (2, 'test1', 'bar', NULL, NULL);

INSERT INTO tags (repo_id, name, digest, pushed_at) VALUES (1, 'latest', 'sha256:8a9217f1887083297faf37cb2c1808f71289f0cd722d6e5157a07be1c362945f', 2);
