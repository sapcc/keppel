INSERT INTO accounts (name, auth_tenant_id, upstream_peer_hostname, required_labels) VALUES ('test1', 'test1authtenant', '', '');

INSERT INTO repos (id, account_name, name) VALUES (1, 'test1', 'foo');
