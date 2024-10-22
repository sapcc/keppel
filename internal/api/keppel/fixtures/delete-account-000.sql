INSERT INTO accounts (name, auth_tenant_id, next_blob_sweep_at) VALUES ('test1', 'tenant1', 200);
INSERT INTO accounts (name, auth_tenant_id) VALUES ('test2', 'tenant2');
INSERT INTO accounts (name, auth_tenant_id) VALUES ('test3', 'tenant3');

INSERT INTO blob_mounts (blob_id, repo_id) VALUES (1, 1);
INSERT INTO blob_mounts (blob_id, repo_id) VALUES (2, 1);
INSERT INTO blob_mounts (blob_id, repo_id) VALUES (3, 1);

INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, next_validation_at) VALUES (1, 'test1', 'sha256:442f91fa9998460f28e8ff7023e5ddca679f7d2b51dc5498e8aba249678cc7f8', 1048919, '6b86b273ff34fce19d6b804eff5a3f5747ada4eaa22f1d49c01e52ddb7875b4b', 0, 604800);
INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, next_validation_at) VALUES (2, 'test1', 'sha256:3ae14a50df760250f0e97faf429cc4541c832ed0de61ad5b6ac25d1d695d1a6e', 1048919, 'd4735e3a265e16eee03f59718b9b5d03019c07d8b6c51f90da3a666eec13ab35', 1, 604801);
INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, next_validation_at) VALUES (3, 'test1', 'sha256:92b29e540b6fcadd4e07525af1546c7eff1bb9a8ef0ef249e0b234cdb13dbea3', 1412, '4e07408562bedb8b60ce05c1decfe3ad16b72230967de01f640b7e4729b49fce', 2, 604802);

INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES (1, 'sha256:e255ca60e7cfef94adfcd95d78f1eb44404c4f5887cbf506dd5799489a42606c', 1);
INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES (1, 'sha256:e255ca60e7cfef94adfcd95d78f1eb44404c4f5887cbf506dd5799489a42606c', 2);
INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES (1, 'sha256:e255ca60e7cfef94adfcd95d78f1eb44404c4f5887cbf506dd5799489a42606c', 3);

INSERT INTO manifest_contents (repo_id, digest, content) VALUES (1, 'sha256:6aa9f3d5659c999fecab6df26efb864792763a2c7ae7580edf5dc11df2882ea5', '{"manifests":[{"digest":"sha256:e255ca60e7cfef94adfcd95d78f1eb44404c4f5887cbf506dd5799489a42606c","mediaType":"application/vnd.docker.distribution.manifest.v2+json","platform":{"architecture":"amd64","os":"linux"},"size":592}],"mediaType":"application/vnd.docker.distribution.manifest.list.v2+json","schemaVersion":2}');

INSERT INTO manifest_manifest_refs (repo_id, parent_digest, child_digest) VALUES (1, 'sha256:6aa9f3d5659c999fecab6df26efb864792763a2c7ae7580edf5dc11df2882ea5', 'sha256:e255ca60e7cfef94adfcd95d78f1eb44404c4f5887cbf506dd5799489a42606c');

INSERT INTO manifests (repo_id, digest, media_type, size_bytes, pushed_at, next_validation_at) VALUES (1, 'sha256:6aa9f3d5659c999fecab6df26efb864792763a2c7ae7580edf5dc11df2882ea5', 'application/vnd.docker.distribution.manifest.list.v2+json', 909, 0, 86400);
INSERT INTO manifests (repo_id, digest, media_type, size_bytes, pushed_at, next_validation_at) VALUES (1, 'sha256:e255ca60e7cfef94adfcd95d78f1eb44404c4f5887cbf506dd5799489a42606c', 'application/vnd.docker.distribution.manifest.v2+json', 592, 100, 86500);

INSERT INTO repos (id, account_name, name) VALUES (1, 'test1', 'foo/bar');
INSERT INTO repos (id, account_name, name) VALUES (2, 'test1', 'something-else');

INSERT INTO trivy_security_info (repo_id, digest, vuln_status, message, next_check_at) VALUES (1, 'sha256:6aa9f3d5659c999fecab6df26efb864792763a2c7ae7580edf5dc11df2882ea5', 'Pending', '', 0);
INSERT INTO trivy_security_info (repo_id, digest, vuln_status, message, next_check_at) VALUES (1, 'sha256:e255ca60e7cfef94adfcd95d78f1eb44404c4f5887cbf506dd5799489a42606c', 'Pending', '', 0);
