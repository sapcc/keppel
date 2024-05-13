INSERT INTO accounts (name, auth_tenant_id) VALUES ('test1', 'test1authtenant');

INSERT INTO manifest_contents (repo_id, digest, content) VALUES (1, 'sha256:8a9217f1887083297faf37cb2c1808f71289f0cd722d6e5157a07be1c362945f', '{"config":{"digest":"sha256:712dfd307e9f735a037e1391f16c8747e7fb0d1318851e32591b51a6bc600c2d","mediaType":"application/vnd.docker.container.image.v1+json","size":1102},"layers":[],"mediaType":"application/vnd.docker.distribution.manifest.v2+json","schemaVersion":2}');

INSERT INTO manifests (repo_id, digest, media_type, size_bytes, pushed_at, validation_error_message, next_validation_at) VALUES (1, 'sha256:8a9217f1887083297faf37cb2c1808f71289f0cd722d6e5157a07be1c362945f', 'application/vnd.docker.distribution.manifest.v2+json', 1367, 3600, 'manifest blob unknown to registry: sha256:712dfd307e9f735a037e1391f16c8747e7fb0d1318851e32591b51a6bc600c2d', 133800);

INSERT INTO quotas (auth_tenant_id, manifests) VALUES ('test1authtenant', 100);

INSERT INTO repos (id, account_name, name) VALUES (1, 'test1', 'foo');

INSERT INTO trivy_security_info (repo_id, digest, vuln_status, message, next_check_at) VALUES (1, 'sha256:8a9217f1887083297faf37cb2c1808f71289f0cd722d6e5157a07be1c362945f', 'Pending', '', 0);
