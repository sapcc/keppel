INSERT INTO accounts (name, auth_tenant_id, registry_http_secret) VALUES ('test1', 'test1authtenant', 'topsecret');

INSERT INTO repos (id, account_name, name) VALUES (1, 'test1', 'foo');
