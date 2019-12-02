INSERT INTO accounts (name, auth_tenant_id, registry_http_secret) VALUES ('test1', 'test1authtenant', 'topsecret');

INSERT INTO manifests (repo_id, digest, media_type, size_bytes) VALUES (1, 'sha256:65147aad93781ff7377b8fb81dab153bd58ffe05b5dc00b67b3035fa9420d2de', 'application/vnd.docker.distribution.manifest.v2+json', 1783);
INSERT INTO manifests (repo_id, digest, media_type, size_bytes) VALUES (1, 'sha256:86fa8722ca7f27e97e1bc5060c3f6720bf43840f143f813fcbe48ed4cbeebb90', 'application/vnd.docker.distribution.manifest.v2+json', 1751);

INSERT INTO repos (id, account_name, name) VALUES (1, 'test1', 'foo');

INSERT INTO tags (repo_id, name, digest) VALUES (1, 'latest', 'sha256:65147aad93781ff7377b8fb81dab153bd58ffe05b5dc00b67b3035fa9420d2de');
