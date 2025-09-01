INSERT INTO accounts (name, auth_tenant_id) VALUES ('test1', 'tenant1');
INSERT INTO accounts (name, auth_tenant_id) VALUES ('test2', 'tenant2');

INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, next_validation_at) VALUES (1, 'test1', 'sha256:0f8191f0b7f4d878acd87097ff96e338a21a5420343221d60a135de41c721782', 2000, '', 1010, 605810);
INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, next_validation_at) VALUES (10, 'test1', 'sha256:99d139f8e68d538eaea0dd594c42503631e46a90296431b43f09fd122df37094', 20000, '', 1100, 605900);
INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, media_type, next_validation_at) VALUES (11, 'test1', 'sha256:74234e98afe7498fb5daf1f36ac2d78acc339464f950703b8c019892f982b90b', 4, '6b86b273ff34fce19d6b804eff5a3f5747ada4eaa22f1d49c01e52ddb7875b4b', 0, 'application/vnd.oci.image.manifest.v1+json', 604800);
INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, next_validation_at) VALUES (2, 'test1', 'sha256:648f958255f6c400e6d202c510234b53558d0739b556912ae7f9c6dbcbb68e92', 4000, '', 1020, 605820);
INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, next_validation_at) VALUES (3, 'test1', 'sha256:cd6a25c2ca3f53fdbbb4beb357ec3b82ec0b4a2abb5eacb93bdc0d2ddc21aa10', 6000, '', 1030, 605830);
INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, next_validation_at) VALUES (4, 'test1', 'sha256:576d7e58c644f5640145f49d26e81d485a903b0c571b948b1288415447b5a9f7', 8000, '', 1040, 605840);
INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, next_validation_at) VALUES (5, 'test1', 'sha256:2af3027165de24c11004942e4d349fcfc0eb550a1ab2fce384f8079e9e96b1a9', 10000, '', 1050, 605850);
INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, next_validation_at) VALUES (6, 'test1', 'sha256:6589e5f5509718c0aa2331ddd8cfc3b13de1591e62b90f0fe2faf6cd0241cf4d', 12000, '', 1060, 605860);
INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, next_validation_at) VALUES (7, 'test1', 'sha256:fe7bc6ce7e3ae806f50c22479236ef2735c69096e772b0b7bc12cff32f3301cf', 14000, '', 1070, 605870);
INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, next_validation_at) VALUES (8, 'test1', 'sha256:2adb29cad7bd5b1dd9aac5f71a08574e0b9c6e150bc81ff133e0f6b61bb58ccc', 16000, '', 1080, 605880);
INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, next_validation_at) VALUES (9, 'test1', 'sha256:ae77c2ac9b830873195a42fd24c0001b2906da9e25a429dde9a67b6f8611d1ec', 18000, '', 1090, 605890);

INSERT INTO quotas (auth_tenant_id, manifests) VALUES ('tenant1', 100);
INSERT INTO quotas (auth_tenant_id, manifests) VALUES ('tenant2', 100);

INSERT INTO repos (id, account_name, name) VALUES (10, 'test2', 'repo2-5');
INSERT INTO repos (id, account_name, name) VALUES (2, 'test2', 'repo2-1');
INSERT INTO repos (id, account_name, name) VALUES (3, 'test1', 'repo1-2');
INSERT INTO repos (id, account_name, name) VALUES (4, 'test2', 'repo2-2');
INSERT INTO repos (id, account_name, name) VALUES (6, 'test2', 'repo2-3');
INSERT INTO repos (id, account_name, name) VALUES (7, 'test1', 'repo1-4');
INSERT INTO repos (id, account_name, name) VALUES (8, 'test2', 'repo2-4');
INSERT INTO repos (id, account_name, name) VALUES (9, 'test1', 'repo1-5');
