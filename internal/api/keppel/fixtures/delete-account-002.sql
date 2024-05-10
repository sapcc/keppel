INSERT INTO accounts (name, auth_tenant_id, next_blob_sweep_at, in_maintenance) VALUES ('test1', 'tenant1', 300, TRUE);
INSERT INTO accounts (name, auth_tenant_id, in_maintenance) VALUES ('test2', 'tenant2', TRUE);
INSERT INTO accounts (name, auth_tenant_id, in_maintenance) VALUES ('test3', 'tenant3', TRUE);

INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, can_be_deleted_at, next_validation_at) VALUES (1, 'test1', 'sha256:442f91fa9998460f28e8ff7023e5ddca679f7d2b51dc5498e8aba249678cc7f8', 1048919, '6b86b273ff34fce19d6b804eff5a3f5747ada4eaa22f1d49c01e52ddb7875b4b', 0, 300, 604800);
INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, can_be_deleted_at, next_validation_at) VALUES (2, 'test1', 'sha256:3ae14a50df760250f0e97faf429cc4541c832ed0de61ad5b6ac25d1d695d1a6e', 1048919, 'd4735e3a265e16eee03f59718b9b5d03019c07d8b6c51f90da3a666eec13ab35', 1, 300, 604801);
INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, can_be_deleted_at, next_validation_at) VALUES (3, 'test1', 'sha256:92b29e540b6fcadd4e07525af1546c7eff1bb9a8ef0ef249e0b234cdb13dbea3', 1412, '4e07408562bedb8b60ce05c1decfe3ad16b72230967de01f640b7e4729b49fce', 2, 300, 604802);
