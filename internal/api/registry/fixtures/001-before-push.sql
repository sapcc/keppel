INSERT INTO accounts (name, auth_tenant_id, upstream_peer_hostname, required_labels) VALUES ('test1', 'test1authtenant', '', '');

INSERT INTO quotas (auth_tenant_id, manifests) VALUES ('test1authtenant', 100);
