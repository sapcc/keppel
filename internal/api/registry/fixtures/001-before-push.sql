INSERT INTO accounts (name, auth_tenant_id, upstream_peer_hostname, required_labels, blobs_sweeped_at, storage_sweeped_at) VALUES ('test1', 'test1authtenant', '', '', NULL, NULL);

INSERT INTO quotas (auth_tenant_id, manifests) VALUES ('test1authtenant', 100);

INSERT INTO repos (id, account_name, name, blob_mounts_sweeped_at) VALUES (1, 'test1', 'foo', NULL);
