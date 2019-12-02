INSERT INTO accounts (name, auth_tenant_id, registry_http_secret) VALUES ('test1', 'test1authtenant', 'topsecret');

INSERT INTO manifests (repo_id, digest, media_type, size_bytes) VALUES (1, 'sha256:6adce611145c988660e2625db76f4ec123ed08c859f5b9e83b20cd08fd664ab6', 'application/vnd.docker.distribution.manifest.v2+json', 265);

INSERT INTO repos (id, account_name, name) VALUES (1, 'test1', 'foo');

INSERT INTO tags (repo_id, name, digest) VALUES (1, 'latest', 'sha256:6adce611145c988660e2625db76f4ec123ed08c859f5b9e83b20cd08fd664ab6');
