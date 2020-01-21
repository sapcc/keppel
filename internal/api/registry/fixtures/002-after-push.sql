INSERT INTO accounts (name, auth_tenant_id, registry_http_secret, upstream_peer_hostname) VALUES ('test1', 'test1authtenant', 'topsecret', '');

INSERT INTO manifests (repo_id, digest, media_type, size_bytes, pushed_at) VALUES (1, 'sha256:86fa8722ca7f27e97e1bc5060c3f6720bf43840f143f813fcbe48ed4cbeebb90', 'application/vnd.docker.distribution.manifest.v2+json', 1751, 3);

INSERT INTO quotas (auth_tenant_id, manifests) VALUES ('test1authtenant', 100);

INSERT INTO repos (id, account_name, name) VALUES (1, 'test1', 'foo');

INSERT INTO tags (repo_id, name, digest, pushed_at) VALUES (1, 'latest', 'sha256:86fa8722ca7f27e97e1bc5060c3f6720bf43840f143f813fcbe48ed4cbeebb90', 3);
