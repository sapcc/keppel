INSERT INTO accounts (name, auth_tenant_id) VALUES ('test1', 'test1authtenant');

INSERT INTO blob_mounts (blob_id, repo_id) VALUES (1, 1);
INSERT INTO blob_mounts (blob_id, repo_id) VALUES (1, 6);

INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, validated_at, media_type) VALUES (1, 'test1', 'sha256:712dfd307e9f735a037e1391f16c8747e7fb0d1318851e32591b51a6bc600c2d', 1102, '6b86b273ff34fce19d6b804eff5a3f5747ada4eaa22f1d49c01e52ddb7875b4b', 0, 0, 'application/vnd.docker.container.image.v1+json');

INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES (1, 'sha256:8a9217f1887083297faf37cb2c1808f71289f0cd722d6e5157a07be1c362945f', 1);

INSERT INTO manifest_contents (repo_id, digest, content) VALUES (1, 'sha256:8a9217f1887083297faf37cb2c1808f71289f0cd722d6e5157a07be1c362945f', '{"config":{"digest":"sha256:712dfd307e9f735a037e1391f16c8747e7fb0d1318851e32591b51a6bc600c2d","mediaType":"application/vnd.docker.container.image.v1+json","size":1102},"layers":[],"mediaType":"application/vnd.docker.distribution.manifest.v2+json","schemaVersion":2}');

INSERT INTO manifests (repo_id, digest, media_type, size_bytes, pushed_at, validated_at) VALUES (1, 'sha256:8a9217f1887083297faf37cb2c1808f71289f0cd722d6e5157a07be1c362945f', 'application/vnd.docker.distribution.manifest.v2+json', 1367, 2, 2);

INSERT INTO quotas (auth_tenant_id, manifests) VALUES ('test1authtenant', 100);

INSERT INTO repos (id, account_name, name) VALUES (1, 'test1', 'foo');
INSERT INTO repos (id, account_name, name) VALUES (6, 'test1', 'bar');

INSERT INTO trivy_security_info (repo_id, digest, vuln_status, message, next_check_at) VALUES (1, 'sha256:8a9217f1887083297faf37cb2c1808f71289f0cd722d6e5157a07be1c362945f', 'Pending', '', 2);
