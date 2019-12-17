INSERT INTO accounts (name, auth_tenant_id, registry_http_secret) VALUES ('test1', 'test1authtenant', 'topsecret');

INSERT INTO quotas (auth_tenant_id, manifests) VALUES ('test1authtenant', 100);
