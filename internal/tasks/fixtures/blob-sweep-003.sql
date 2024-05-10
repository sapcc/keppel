INSERT INTO accounts (name, auth_tenant_id, next_blob_sweep_at) VALUES ('test1', 'test1authtenant', 21600);

INSERT INTO blob_mounts (blob_id, repo_id) VALUES (3, 1);
INSERT INTO blob_mounts (blob_id, repo_id) VALUES (4, 1);
INSERT INTO blob_mounts (blob_id, repo_id) VALUES (5, 1);

INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, next_validation_at) VALUES (3, 'test1', 'sha256:3ae14a50df760250f0e97faf429cc4541c832ed0de61ad5b6ac25d1d695d1a6e', 1048919, '4e07408562bedb8b60ce05c1decfe3ad16b72230967de01f640b7e4729b49fce', 3600, 608400);
INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, next_validation_at) VALUES (4, 'test1', 'sha256:5c82ce800cfc28b78e32a606bcae512145273f6ab39a2b114879beb992329b53', 1048919, '4b227777d4dd1fc61c6f884f48641d02b4d121d3fd328cb08b5531fcacdabf8a', 3600, 608400);
INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, next_validation_at) VALUES (5, 'test1', 'sha256:17715f3edba77811b09cc23421c88f0b904c33871f526a1a0a426ad309cbb8ad', 1048919, 'ef2d127de37b942baad06145e54b0c619a1f22327b2ebbcfbec78f5564afe39d', 3600, 608400);

INSERT INTO quotas (auth_tenant_id, manifests) VALUES ('test1authtenant', 100);

INSERT INTO repos (id, account_name, name) VALUES (1, 'test1', 'foo');
