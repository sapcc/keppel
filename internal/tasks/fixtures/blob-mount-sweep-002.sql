INSERT INTO accounts (name, auth_tenant_id) VALUES ('test1', 'test1authtenant');

INSERT INTO blob_mounts (blob_id, repo_id) VALUES (1, 1);
INSERT INTO blob_mounts (blob_id, repo_id) VALUES (2, 1);
INSERT INTO blob_mounts (blob_id, repo_id) VALUES (3, 1);
INSERT INTO blob_mounts (blob_id, repo_id) VALUES (4, 1);
INSERT INTO blob_mounts (blob_id, repo_id) VALUES (5, 1);

INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, media_type, next_validation_at) VALUES (1, 'test1', 'sha256:442f91fa9998460f28e8ff7023e5ddca679f7d2b51dc5498e8aba249678cc7f8', 1048919, '6b86b273ff34fce19d6b804eff5a3f5747ada4eaa22f1d49c01e52ddb7875b4b', 3600, 'application/vnd.docker.image.rootfs.diff.tar.gzip', 608400);
INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, media_type, next_validation_at) VALUES (2, 'test1', 'sha256:3ae14a50df760250f0e97faf429cc4541c832ed0de61ad5b6ac25d1d695d1a6e', 1048919, 'd4735e3a265e16eee03f59718b9b5d03019c07d8b6c51f90da3a666eec13ab35', 3600, 'application/vnd.docker.image.rootfs.diff.tar.gzip', 608400);
INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, media_type, next_validation_at) VALUES (3, 'test1', 'sha256:92b29e540b6fcadd4e07525af1546c7eff1bb9a8ef0ef249e0b234cdb13dbea3', 1412, '4e07408562bedb8b60ce05c1decfe3ad16b72230967de01f640b7e4729b49fce', 3600, 'application/vnd.docker.container.image.v1+json', 608400);
INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, next_validation_at) VALUES (4, 'test1', 'sha256:5c82ce800cfc28b78e32a606bcae512145273f6ab39a2b114879beb992329b53', 1048919, '4b227777d4dd1fc61c6f884f48641d02b4d121d3fd328cb08b5531fcacdabf8a', 10800, 615600);
INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, next_validation_at) VALUES (5, 'test1', 'sha256:17715f3edba77811b09cc23421c88f0b904c33871f526a1a0a426ad309cbb8ad', 1048919, 'ef2d127de37b942baad06145e54b0c619a1f22327b2ebbcfbec78f5564afe39d', 10800, 615600);

INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES (1, 'sha256:e255ca60e7cfef94adfcd95d78f1eb44404c4f5887cbf506dd5799489a42606c', 1);
INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES (1, 'sha256:e255ca60e7cfef94adfcd95d78f1eb44404c4f5887cbf506dd5799489a42606c', 2);
INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES (1, 'sha256:e255ca60e7cfef94adfcd95d78f1eb44404c4f5887cbf506dd5799489a42606c', 3);

INSERT INTO manifest_contents (repo_id, digest, content) VALUES (1, 'sha256:e255ca60e7cfef94adfcd95d78f1eb44404c4f5887cbf506dd5799489a42606c', '{"config":{"digest":"sha256:92b29e540b6fcadd4e07525af1546c7eff1bb9a8ef0ef249e0b234cdb13dbea3","mediaType":"application/vnd.docker.container.image.v1+json","size":1412},"layers":[{"digest":"sha256:442f91fa9998460f28e8ff7023e5ddca679f7d2b51dc5498e8aba249678cc7f8","mediaType":"application/vnd.docker.image.rootfs.diff.tar.gzip","size":1048919},{"digest":"sha256:3ae14a50df760250f0e97faf429cc4541c832ed0de61ad5b6ac25d1d695d1a6e","mediaType":"application/vnd.docker.image.rootfs.diff.tar.gzip","size":1048919}],"mediaType":"application/vnd.docker.distribution.manifest.v2+json","schemaVersion":2}');

INSERT INTO manifests (repo_id, digest, media_type, size_bytes, pushed_at, min_layer_created_at, max_layer_created_at, next_validation_at) VALUES (1, 'sha256:e255ca60e7cfef94adfcd95d78f1eb44404c4f5887cbf506dd5799489a42606c', 'application/vnd.docker.distribution.manifest.v2+json', 2099842, 3600, 1, 1, 90000);

INSERT INTO quotas (auth_tenant_id, manifests) VALUES ('test1authtenant', 100);

INSERT INTO repos (id, account_name, name, next_blob_mount_sweep_at) VALUES (1, 'test1', 'foo', 7200);

INSERT INTO trivy_security_info (repo_id, digest, vuln_status, message, next_check_at) VALUES (1, 'sha256:e255ca60e7cfef94adfcd95d78f1eb44404c4f5887cbf506dd5799489a42606c', 'Pending', '', 3600);
