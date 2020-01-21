INSERT INTO accounts (name, auth_tenant_id, registry_http_secret, upstream_peer_hostname) VALUES ('test1', 'test1authtenant', 'topsecret', '');

INSERT INTO quotas (auth_tenant_id, manifests) VALUES ('test1authtenant', 100);

INSERT INTO repos (id, account_name, name) VALUES (1, 'test1', 'foo');
