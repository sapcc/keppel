INSERT INTO accounts (name, auth_tenant_id, next_blob_sweep_at) VALUES ('test1', 'test1authtenant', 21600);

INSERT INTO blob_mounts (blob_id, repo_id) VALUES (3, 1);
INSERT INTO blob_mounts (blob_id, repo_id) VALUES (4, 1);
INSERT INTO blob_mounts (blob_id, repo_id) VALUES (5, 1);

INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, next_validation_at) VALUES (3, 'test1', 'sha256:7e5e9e18c5d7f426f770339adab8d0d4682823dae447476e19fefef2b0c93610', 1048613, '4e07408562bedb8b60ce05c1decfe3ad16b72230967de01f640b7e4729b49fce', 3600, 608400);
INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, next_validation_at) VALUES (4, 'test1', 'sha256:4ebfd1aafe77056e348c3ef48fa229b60390abb9c15c1023f609d88c06943eab', 1048576, '4b227777d4dd1fc61c6f884f48641d02b4d121d3fd328cb08b5531fcacdabf8a', 3600, 608400);
INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, next_validation_at) VALUES (5, 'test1', 'sha256:40d8c4c0cd13c2855f975aa1694a55634b990e506b0c55f75e560b35b4fdce3c', 1048613, 'ef2d127de37b942baad06145e54b0c619a1f22327b2ebbcfbec78f5564afe39d', 3600, 608400);

INSERT INTO quotas (auth_tenant_id, manifests) VALUES ('test1authtenant', 100);

INSERT INTO repos (id, account_name, name) VALUES (1, 'test1', 'foo');
