INSERT INTO accounts (name, auth_tenant_id) VALUES ('test1', 'test1authtenant');

INSERT INTO blob_mounts (blob_id, repo_id) VALUES (1, 1);
INSERT INTO blob_mounts (blob_id, repo_id) VALUES (2, 1);
INSERT INTO blob_mounts (blob_id, repo_id) VALUES (3, 1);

INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, next_validation_at) VALUES (1, 'test1', 'sha256:2afc94a21f8a7af5b7eac32e3a3acabfd2db3cb80da1631a995eeee413171bc1', 1048919, '6b86b273ff34fce19d6b804eff5a3f5747ada4eaa22f1d49c01e52ddb7875b4b', 3601, 1299601);
INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, next_validation_at) VALUES (2, 'test1', 'sha256:442f91fa9998460f28e8ff7023e5ddca679f7d2b51dc5498e8aba249678cc7f8', 1048919, 'd4735e3a265e16eee03f59718b9b5d03019c07d8b6c51f90da3a666eec13ab35', 3602, 1299602);
INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, next_validation_at) VALUES (3, 'test1', 'sha256:3ae14a50df760250f0e97faf429cc4541c832ed0de61ad5b6ac25d1d695d1a6e', 1048919, '4e07408562bedb8b60ce05c1decfe3ad16b72230967de01f640b7e4729b49fce', 3603, 1299603);

INSERT INTO quotas (auth_tenant_id, manifests) VALUES ('test1authtenant', 100);

INSERT INTO repos (id, account_name, name) VALUES (1, 'test1', 'foo');
