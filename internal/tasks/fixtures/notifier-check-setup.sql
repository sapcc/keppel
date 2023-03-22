INSERT INTO accounts (name, auth_tenant_id) VALUES ('test1', 'test1authtenant');

INSERT INTO blob_mounts (blob_id, repo_id) VALUES (1, 1);
INSERT INTO blob_mounts (blob_id, repo_id) VALUES (2, 1);

INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, validated_at, media_type) VALUES (1, 'test1', 'sha256:2afc94a21f8a7af5b7eac32e3a3acabfd2db3cb80da1631a995eeee413171bc1', 1048919, '6b86b273ff34fce19d6b804eff5a3f5747ada4eaa22f1d49c01e52ddb7875b4b', 3600, 3600, 'application/vnd.docker.image.rootfs.diff.tar.gzip');
INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, validated_at, media_type) VALUES (2, 'test1', 'sha256:09eb8b4e127f21b642d66521f2e76f125fff23437742f832b0fd534e741f4f8b', 1257, 'd4735e3a265e16eee03f59718b9b5d03019c07d8b6c51f90da3a666eec13ab35', 3600, 3600, 'application/vnd.docker.container.image.v1+json');

INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES (1, 'sha256:0962993cac41a5429d58b5142279ea849c3291cdffcc881d0188f1be73928ffd', 1);
INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES (1, 'sha256:0962993cac41a5429d58b5142279ea849c3291cdffcc881d0188f1be73928ffd', 2);

INSERT INTO manifest_contents (repo_id, digest, content) VALUES (1, 'sha256:0962993cac41a5429d58b5142279ea849c3291cdffcc881d0188f1be73928ffd', '{"config":{"digest":"sha256:09eb8b4e127f21b642d66521f2e76f125fff23437742f832b0fd534e741f4f8b","mediaType":"application/vnd.docker.container.image.v1+json","size":1257},"layers":[{"digest":"sha256:2afc94a21f8a7af5b7eac32e3a3acabfd2db3cb80da1631a995eeee413171bc1","mediaType":"application/vnd.docker.image.rootfs.diff.tar.gzip","size":1048919}],"mediaType":"application/vnd.docker.distribution.manifest.v2+json","schemaVersion":2}');

INSERT INTO manifests (repo_id, digest, media_type, size_bytes, pushed_at, validated_at) VALUES (1, 'sha256:0962993cac41a5429d58b5142279ea849c3291cdffcc881d0188f1be73928ffd', 'application/vnd.docker.distribution.manifest.v2+json', 1050604, 3600, 3600);

INSERT INTO quotas (auth_tenant_id, manifests) VALUES ('test1authtenant', 100);

INSERT INTO repos (id, account_name, name) VALUES (1, 'test1', 'foo');

INSERT INTO vuln_info (repo_id, digest, status, message, next_check_at) VALUES (1, 'sha256:0962993cac41a5429d58b5142279ea849c3291cdffcc881d0188f1be73928ffd', 'Pending', '', 3600);
