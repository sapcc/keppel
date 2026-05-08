INSERT INTO accounts (name, auth_tenant_id, external_peer_url, external_peer_username, external_peer_password) VALUES ('test1', 'test1authtenant', 'registry.example.org/test1', 'replication@registry-secondary.example.org', 'a4cb6fae5b8bb91b0b993486937103dab05eca93');

INSERT INTO blob_mounts (blob_id, repo_id) VALUES (1, 1);
INSERT INTO blob_mounts (blob_id, repo_id) VALUES (2, 1);
INSERT INTO blob_mounts (blob_id, repo_id) VALUES (3, 1);
INSERT INTO blob_mounts (blob_id, repo_id) VALUES (4, 1);
INSERT INTO blob_mounts (blob_id, repo_id) VALUES (5, 1);
INSERT INTO blob_mounts (blob_id, repo_id) VALUES (6, 1);
INSERT INTO blob_mounts (blob_id, repo_id) VALUES (7, 1);
INSERT INTO blob_mounts (blob_id, repo_id) VALUES (8, 1);
INSERT INTO blob_mounts (blob_id, repo_id) VALUES (9, 1);

INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, media_type, next_validation_at) VALUES (1, 'test1', 'sha256:6ddfeecfd65021277a78ff9269e3b230a26c9d4dfa5015c5db5befadf5b96ce0', 1412, '6b86b273ff34fce19d6b804eff5a3f5747ada4eaa22f1d49c01e52ddb7875b4b', 3600, 'application/vnd.docker.container.image.v1+json', 608400);
INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, media_type, next_validation_at) VALUES (2, 'test1', 'sha256:eb56d5d5d6a0b061ca49785b5a29e899e68208cdb87853f150e97ecb90d17d92', 1048919, '', 0, 'application/vnd.docker.image.rootfs.diff.tar.gzip', 0);
INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, media_type, next_validation_at) VALUES (3, 'test1', 'sha256:5dfe56df10488d9c1f75414293fe474eaa7fca63acdee374bc371250f1a372b4', 1048576, '', 0, 'application/vnd.docker.image.rootfs.diff.tar', 0);
INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, media_type, next_validation_at) VALUES (4, 'test1', 'sha256:5abbee0f4998f9d26ea5d2c2b1e08c9ae50e8662ce01b74a9448054c87f72ce3', 1412, 'd4735e3a265e16eee03f59718b9b5d03019c07d8b6c51f90da3a666eec13ab35', 3600, 'application/vnd.docker.container.image.v1+json', 608400);
INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, media_type, next_validation_at) VALUES (5, 'test1', 'sha256:4b2ac57bae1cdb45d45edc74ff4b2c28f60434ff3ce128b4e0bd4034528046eb', 1048576, '', 0, 'application/vnd.docker.image.rootfs.diff.tar', 0);
INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, media_type, next_validation_at) VALUES (6, 'test1', 'sha256:759273c0564a61fb253a1b7502176659087094407e61221405362cf0d7c071bc', 1048613, '', 0, 'application/vnd.docker.image.rootfs.diff.tar.zstd', 0);
INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, media_type, next_validation_at) VALUES (7, 'test1', 'sha256:7b43b208093efdf62779eaee768373ff406d5d1b53445a3bbf373e1bd2ea96af', 1412, '4e07408562bedb8b60ce05c1decfe3ad16b72230967de01f640b7e4729b49fce', 3600, 'application/vnd.docker.container.image.v1+json', 608400);
INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, media_type, next_validation_at) VALUES (8, 'test1', 'sha256:908c681fdc861d81d3f2cf3c760b52c66b126f7e54354d93b7df9a8a2b94e3f2', 1048919, '', 0, 'application/vnd.docker.image.rootfs.diff.tar.gzip', 0);
INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, media_type, next_validation_at) VALUES (9, 'test1', 'sha256:d6409efba386750523179fbd80a1c5cfbc96a4db533eeb902028536bcc9f7f36', 1048613, '', 0, 'application/vnd.docker.image.rootfs.diff.tar.zstd', 0);

INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES (1, 'sha256:62756a4cfdd8acb07f8f5b530553e4cd1ac498c6920d6825dd64efc7573dacd7', 1);
INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES (1, 'sha256:62756a4cfdd8acb07f8f5b530553e4cd1ac498c6920d6825dd64efc7573dacd7', 2);
INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES (1, 'sha256:62756a4cfdd8acb07f8f5b530553e4cd1ac498c6920d6825dd64efc7573dacd7', 3);
INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES (1, 'sha256:88832a417bed5304165aad2842f82ed274064e20d4754de91f6ca22207f3be06', 7);
INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES (1, 'sha256:88832a417bed5304165aad2842f82ed274064e20d4754de91f6ca22207f3be06', 8);
INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES (1, 'sha256:88832a417bed5304165aad2842f82ed274064e20d4754de91f6ca22207f3be06', 9);
INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES (1, 'sha256:dd20a096cc64b401afa9a3d759585b88d6c249cbeac465d1586584c059165600', 4);
INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES (1, 'sha256:dd20a096cc64b401afa9a3d759585b88d6c249cbeac465d1586584c059165600', 5);
INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES (1, 'sha256:dd20a096cc64b401afa9a3d759585b88d6c249cbeac465d1586584c059165600', 6);

INSERT INTO manifest_contents (repo_id, digest, content) VALUES (1, 'sha256:3dfe6ccdf3f720b8bc17cdb383e9f8361cd794e30cd473fc2935da039d02243a', '{"manifests":[{"digest":"sha256:62756a4cfdd8acb07f8f5b530553e4cd1ac498c6920d6825dd64efc7573dacd7","mediaType":"application/vnd.docker.distribution.manifest.v2+json","platform":{"architecture":"amd64","os":"linux"},"size":587},{"digest":"sha256:dd20a096cc64b401afa9a3d759585b88d6c249cbeac465d1586584c059165600","mediaType":"application/vnd.docker.distribution.manifest.v2+json","platform":{"architecture":"arm","os":"linux"},"size":587}],"mediaType":"application/vnd.docker.distribution.manifest.list.v2+json","schemaVersion":2}');
INSERT INTO manifest_contents (repo_id, digest, content) VALUES (1, 'sha256:62756a4cfdd8acb07f8f5b530553e4cd1ac498c6920d6825dd64efc7573dacd7', '{"config":{"digest":"sha256:6ddfeecfd65021277a78ff9269e3b230a26c9d4dfa5015c5db5befadf5b96ce0","mediaType":"application/vnd.docker.container.image.v1+json","size":1412},"layers":[{"digest":"sha256:eb56d5d5d6a0b061ca49785b5a29e899e68208cdb87853f150e97ecb90d17d92","mediaType":"application/vnd.docker.image.rootfs.diff.tar.gzip","size":1048919},{"digest":"sha256:5dfe56df10488d9c1f75414293fe474eaa7fca63acdee374bc371250f1a372b4","mediaType":"application/vnd.docker.image.rootfs.diff.tar","size":1048576}],"mediaType":"application/vnd.docker.distribution.manifest.v2+json","schemaVersion":2}');
INSERT INTO manifest_contents (repo_id, digest, content) VALUES (1, 'sha256:88832a417bed5304165aad2842f82ed274064e20d4754de91f6ca22207f3be06', '{"config":{"digest":"sha256:7b43b208093efdf62779eaee768373ff406d5d1b53445a3bbf373e1bd2ea96af","mediaType":"application/vnd.docker.container.image.v1+json","size":1412},"layers":[{"digest":"sha256:908c681fdc861d81d3f2cf3c760b52c66b126f7e54354d93b7df9a8a2b94e3f2","mediaType":"application/vnd.docker.image.rootfs.diff.tar.gzip","size":1048919},{"digest":"sha256:d6409efba386750523179fbd80a1c5cfbc96a4db533eeb902028536bcc9f7f36","mediaType":"application/vnd.docker.image.rootfs.diff.tar.zstd","size":1048613}],"mediaType":"application/vnd.docker.distribution.manifest.v2+json","schemaVersion":2}');
INSERT INTO manifest_contents (repo_id, digest, content) VALUES (1, 'sha256:dd20a096cc64b401afa9a3d759585b88d6c249cbeac465d1586584c059165600', '{"config":{"digest":"sha256:5abbee0f4998f9d26ea5d2c2b1e08c9ae50e8662ce01b74a9448054c87f72ce3","mediaType":"application/vnd.docker.container.image.v1+json","size":1412},"layers":[{"digest":"sha256:4b2ac57bae1cdb45d45edc74ff4b2c28f60434ff3ce128b4e0bd4034528046eb","mediaType":"application/vnd.docker.image.rootfs.diff.tar","size":1048576},{"digest":"sha256:759273c0564a61fb253a1b7502176659087094407e61221405362cf0d7c071bc","mediaType":"application/vnd.docker.image.rootfs.diff.tar.zstd","size":1048613}],"mediaType":"application/vnd.docker.distribution.manifest.v2+json","schemaVersion":2}');

INSERT INTO manifest_manifest_refs (repo_id, parent_digest, child_digest) VALUES (1, 'sha256:3dfe6ccdf3f720b8bc17cdb383e9f8361cd794e30cd473fc2935da039d02243a', 'sha256:62756a4cfdd8acb07f8f5b530553e4cd1ac498c6920d6825dd64efc7573dacd7');
INSERT INTO manifest_manifest_refs (repo_id, parent_digest, child_digest) VALUES (1, 'sha256:3dfe6ccdf3f720b8bc17cdb383e9f8361cd794e30cd473fc2935da039d02243a', 'sha256:dd20a096cc64b401afa9a3d759585b88d6c249cbeac465d1586584c059165600');

INSERT INTO manifests (repo_id, digest, media_type, size_bytes, pushed_at, min_layer_created_at, max_layer_created_at, next_validation_at) VALUES (1, 'sha256:3dfe6ccdf3f720b8bc17cdb383e9f8361cd794e30cd473fc2935da039d02243a', 'application/vnd.docker.distribution.manifest.list.v2+json', 4199209, 3600, 1, 1, 90000);
INSERT INTO manifests (repo_id, digest, media_type, size_bytes, pushed_at, last_pulled_at, min_layer_created_at, max_layer_created_at, next_validation_at) VALUES (1, 'sha256:62756a4cfdd8acb07f8f5b530553e4cd1ac498c6920d6825dd64efc7573dacd7', 'application/vnd.docker.distribution.manifest.v2+json', 2099494, 3600, 32, 1, 1, 90000);
INSERT INTO manifests (repo_id, digest, media_type, size_bytes, pushed_at, last_pulled_at, min_layer_created_at, max_layer_created_at, next_validation_at) VALUES (1, 'sha256:88832a417bed5304165aad2842f82ed274064e20d4754de91f6ca22207f3be06', 'application/vnd.docker.distribution.manifest.v2+json', 2099536, 3600, 42, 1, 1, 90000);
INSERT INTO manifests (repo_id, digest, media_type, size_bytes, pushed_at, last_pulled_at, min_layer_created_at, max_layer_created_at, next_validation_at) VALUES (1, 'sha256:dd20a096cc64b401afa9a3d759585b88d6c249cbeac465d1586584c059165600', 'application/vnd.docker.distribution.manifest.v2+json', 2099188, 3600, 52, 1, 1, 90000);

INSERT INTO peers (hostname, our_password) VALUES ('registry.example.org', 'a4cb6fae5b8bb91b0b993486937103dab05eca93');

INSERT INTO quotas (auth_tenant_id, manifests) VALUES ('test1authtenant', 100);

INSERT INTO repos (id, account_name, name) VALUES (1, 'test1', 'foo');

INSERT INTO tags (repo_id, name, digest, pushed_at, last_pulled_at) VALUES (1, 'latest', 'sha256:62756a4cfdd8acb07f8f5b530553e4cd1ac498c6920d6825dd64efc7573dacd7', 3600, 32);
INSERT INTO tags (repo_id, name, digest, pushed_at, last_pulled_at) VALUES (1, 'other', 'sha256:62756a4cfdd8acb07f8f5b530553e4cd1ac498c6920d6825dd64efc7573dacd7', 3600, 52);

INSERT INTO trivy_security_info (repo_id, digest, vuln_status, message, next_check_at) VALUES (1, 'sha256:3dfe6ccdf3f720b8bc17cdb383e9f8361cd794e30cd473fc2935da039d02243a', 'Pending', '', 3600);
INSERT INTO trivy_security_info (repo_id, digest, vuln_status, message, next_check_at) VALUES (1, 'sha256:62756a4cfdd8acb07f8f5b530553e4cd1ac498c6920d6825dd64efc7573dacd7', 'Pending', '', 3600);
INSERT INTO trivy_security_info (repo_id, digest, vuln_status, message, next_check_at) VALUES (1, 'sha256:88832a417bed5304165aad2842f82ed274064e20d4754de91f6ca22207f3be06', 'Pending', '', 3600);
INSERT INTO trivy_security_info (repo_id, digest, vuln_status, message, next_check_at) VALUES (1, 'sha256:dd20a096cc64b401afa9a3d759585b88d6c249cbeac465d1586584c059165600', 'Pending', '', 3600);
