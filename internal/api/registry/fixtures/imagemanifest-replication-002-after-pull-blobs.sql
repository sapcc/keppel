INSERT INTO accounts (name, auth_tenant_id, upstream_peer_hostname) VALUES ('test1', 'test1authtenant', 'registry.example.org');

INSERT INTO blob_mounts (blob_id, repo_id) VALUES (1, 1);
INSERT INTO blob_mounts (blob_id, repo_id) VALUES (2, 1);

INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, media_type, next_validation_at) VALUES (1, 'test1', 'sha256:a0a84c915810634c0d4522dca789fa95a7ad5b843860ead04d2e13ec949d8a2f', 1257, '6b86b273ff34fce19d6b804eff5a3f5747ada4eaa22f1d49c01e52ddb7875b4b', 2, 'application/vnd.docker.container.image.v1+json', 604802);
INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, media_type, next_validation_at) VALUES (2, 'test1', 'sha256:442f91fa9998460f28e8ff7023e5ddca679f7d2b51dc5498e8aba249678cc7f8', 1048919, 'd4735e3a265e16eee03f59718b9b5d03019c07d8b6c51f90da3a666eec13ab35', 3, 'application/vnd.docker.image.rootfs.diff.tar.gzip', 604803);

INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES (1, 'sha256:e3c1e46560a7ce30e3d107791e1f60a588eda9554564a5d17aa365e53dd6ae58', 1);
INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES (1, 'sha256:e3c1e46560a7ce30e3d107791e1f60a588eda9554564a5d17aa365e53dd6ae58', 2);

INSERT INTO manifest_contents (repo_id, digest, content) VALUES (1, 'sha256:e3c1e46560a7ce30e3d107791e1f60a588eda9554564a5d17aa365e53dd6ae58', '{"config":{"digest":"sha256:a0a84c915810634c0d4522dca789fa95a7ad5b843860ead04d2e13ec949d8a2f","mediaType":"application/vnd.docker.container.image.v1+json","size":1257},"layers":[{"digest":"sha256:442f91fa9998460f28e8ff7023e5ddca679f7d2b51dc5498e8aba249678cc7f8","mediaType":"application/vnd.docker.image.rootfs.diff.tar.gzip","size":1048919}],"mediaType":"application/vnd.docker.distribution.manifest.v2+json","schemaVersion":2}');

INSERT INTO manifests (repo_id, digest, media_type, size_bytes, pushed_at, last_pulled_at, next_validation_at) VALUES (1, 'sha256:e3c1e46560a7ce30e3d107791e1f60a588eda9554564a5d17aa365e53dd6ae58', 'application/vnd.docker.distribution.manifest.v2+json', 1050604, 2, 2, 86402);

INSERT INTO peers (hostname, our_password) VALUES ('registry.example.org', 'a4cb6fae5b8bb91b0b993486937103dab05eca93');

INSERT INTO quotas (auth_tenant_id, manifests) VALUES ('test1authtenant', 100);

INSERT INTO repos (id, account_name, name) VALUES (1, 'test1', 'foo');

INSERT INTO trivy_security_info (repo_id, digest, vuln_status, message, next_check_at) VALUES (1, 'sha256:e3c1e46560a7ce30e3d107791e1f60a588eda9554564a5d17aa365e53dd6ae58', 'Pending', '', 2);
