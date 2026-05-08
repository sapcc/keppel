INSERT INTO accounts (name, auth_tenant_id, next_storage_sweep_at) VALUES ('test1', 'test1authtenant', 25200);

INSERT INTO blob_mounts (blob_id, repo_id) VALUES (1, 1);
INSERT INTO blob_mounts (blob_id, repo_id) VALUES (2, 1);
INSERT INTO blob_mounts (blob_id, repo_id) VALUES (3, 1);
INSERT INTO blob_mounts (blob_id, repo_id) VALUES (4, 1);
INSERT INTO blob_mounts (blob_id, repo_id) VALUES (5, 1);
INSERT INTO blob_mounts (blob_id, repo_id) VALUES (6, 1);

INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, media_type, next_validation_at) VALUES (1, 'test1', 'sha256:442f91fa9998460f28e8ff7023e5ddca679f7d2b51dc5498e8aba249678cc7f8', 1048919, '6b86b273ff34fce19d6b804eff5a3f5747ada4eaa22f1d49c01e52ddb7875b4b', 3600, 'application/vnd.docker.image.rootfs.diff.tar.gzip', 608400);
INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, media_type, next_validation_at) VALUES (2, 'test1', 'sha256:7e5e9e18c5d7f426f770339adab8d0d4682823dae447476e19fefef2b0c93610', 1048613, 'd4735e3a265e16eee03f59718b9b5d03019c07d8b6c51f90da3a666eec13ab35', 3600, 'application/vnd.docker.image.rootfs.diff.tar.zstd', 608400);
INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, media_type, next_validation_at) VALUES (3, 'test1', 'sha256:dfc7f39bd601c3aa914a526c93a39f757fb3401b506877279bc091dd0be50cc1', 1412, '4e07408562bedb8b60ce05c1decfe3ad16b72230967de01f640b7e4729b49fce', 3600, 'application/vnd.docker.container.image.v1+json', 608400);
INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, media_type, next_validation_at) VALUES (4, 'test1', 'sha256:eb56d5d5d6a0b061ca49785b5a29e899e68208cdb87853f150e97ecb90d17d92', 1048919, '4b227777d4dd1fc61c6f884f48641d02b4d121d3fd328cb08b5531fcacdabf8a', 3600, 'application/vnd.docker.image.rootfs.diff.tar.gzip', 608400);
INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, media_type, next_validation_at) VALUES (5, 'test1', 'sha256:5dfe56df10488d9c1f75414293fe474eaa7fca63acdee374bc371250f1a372b4', 1048576, 'ef2d127de37b942baad06145e54b0c619a1f22327b2ebbcfbec78f5564afe39d', 3600, 'application/vnd.docker.image.rootfs.diff.tar', 608400);
INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, media_type, next_validation_at) VALUES (6, 'test1', 'sha256:6ddfeecfd65021277a78ff9269e3b230a26c9d4dfa5015c5db5befadf5b96ce0', 1412, 'e7f6c011776e8db7cd330b54174fd76f7d0216b612387a5ffcfb81e6f0919683', 3600, 'application/vnd.docker.container.image.v1+json', 608400);

INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES (1, 'sha256:01ec57127f7fa074c90f977d96254e80f18c10ef54e0cc5ca63e59c079458927', 1);
INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES (1, 'sha256:01ec57127f7fa074c90f977d96254e80f18c10ef54e0cc5ca63e59c079458927', 2);
INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES (1, 'sha256:01ec57127f7fa074c90f977d96254e80f18c10ef54e0cc5ca63e59c079458927', 3);
INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES (1, 'sha256:62756a4cfdd8acb07f8f5b530553e4cd1ac498c6920d6825dd64efc7573dacd7', 4);
INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES (1, 'sha256:62756a4cfdd8acb07f8f5b530553e4cd1ac498c6920d6825dd64efc7573dacd7', 5);
INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES (1, 'sha256:62756a4cfdd8acb07f8f5b530553e4cd1ac498c6920d6825dd64efc7573dacd7', 6);

INSERT INTO manifest_contents (repo_id, digest, content) VALUES (1, 'sha256:01ec57127f7fa074c90f977d96254e80f18c10ef54e0cc5ca63e59c079458927', '{"config":{"digest":"sha256:dfc7f39bd601c3aa914a526c93a39f757fb3401b506877279bc091dd0be50cc1","mediaType":"application/vnd.docker.container.image.v1+json","size":1412},"layers":[{"digest":"sha256:442f91fa9998460f28e8ff7023e5ddca679f7d2b51dc5498e8aba249678cc7f8","mediaType":"application/vnd.docker.image.rootfs.diff.tar.gzip","size":1048919},{"digest":"sha256:7e5e9e18c5d7f426f770339adab8d0d4682823dae447476e19fefef2b0c93610","mediaType":"application/vnd.docker.image.rootfs.diff.tar.zstd","size":1048613}],"mediaType":"application/vnd.docker.distribution.manifest.v2+json","schemaVersion":2}');
INSERT INTO manifest_contents (repo_id, digest, content) VALUES (1, 'sha256:62756a4cfdd8acb07f8f5b530553e4cd1ac498c6920d6825dd64efc7573dacd7', '{"config":{"digest":"sha256:6ddfeecfd65021277a78ff9269e3b230a26c9d4dfa5015c5db5befadf5b96ce0","mediaType":"application/vnd.docker.container.image.v1+json","size":1412},"layers":[{"digest":"sha256:eb56d5d5d6a0b061ca49785b5a29e899e68208cdb87853f150e97ecb90d17d92","mediaType":"application/vnd.docker.image.rootfs.diff.tar.gzip","size":1048919},{"digest":"sha256:5dfe56df10488d9c1f75414293fe474eaa7fca63acdee374bc371250f1a372b4","mediaType":"application/vnd.docker.image.rootfs.diff.tar","size":1048576}],"mediaType":"application/vnd.docker.distribution.manifest.v2+json","schemaVersion":2}');
INSERT INTO manifest_contents (repo_id, digest, content) VALUES (1, 'sha256:65376fe20a63088d572f9dbcd2b11cc5408805d47d849d4d288278f5e56b0bd6', '{"manifests":[{"digest":"sha256:01ec57127f7fa074c90f977d96254e80f18c10ef54e0cc5ca63e59c079458927","mediaType":"application/vnd.docker.distribution.manifest.v2+json","platform":{"architecture":"amd64","os":"linux"},"size":592},{"digest":"sha256:62756a4cfdd8acb07f8f5b530553e4cd1ac498c6920d6825dd64efc7573dacd7","mediaType":"application/vnd.docker.distribution.manifest.v2+json","platform":{"architecture":"arm","os":"linux"},"size":587}],"mediaType":"application/vnd.docker.distribution.manifest.list.v2+json","schemaVersion":2}');

INSERT INTO manifest_manifest_refs (repo_id, parent_digest, child_digest) VALUES (1, 'sha256:65376fe20a63088d572f9dbcd2b11cc5408805d47d849d4d288278f5e56b0bd6', 'sha256:01ec57127f7fa074c90f977d96254e80f18c10ef54e0cc5ca63e59c079458927');
INSERT INTO manifest_manifest_refs (repo_id, parent_digest, child_digest) VALUES (1, 'sha256:65376fe20a63088d572f9dbcd2b11cc5408805d47d849d4d288278f5e56b0bd6', 'sha256:62756a4cfdd8acb07f8f5b530553e4cd1ac498c6920d6825dd64efc7573dacd7');

INSERT INTO manifests (repo_id, digest, media_type, size_bytes, pushed_at, min_layer_created_at, max_layer_created_at, next_validation_at) VALUES (1, 'sha256:01ec57127f7fa074c90f977d96254e80f18c10ef54e0cc5ca63e59c079458927', 'application/vnd.docker.distribution.manifest.v2+json', 2099536, 3600, 1, 1, 90000);
INSERT INTO manifests (repo_id, digest, media_type, size_bytes, pushed_at, min_layer_created_at, max_layer_created_at, next_validation_at) VALUES (1, 'sha256:62756a4cfdd8acb07f8f5b530553e4cd1ac498c6920d6825dd64efc7573dacd7', 'application/vnd.docker.distribution.manifest.v2+json', 2099494, 3600, 1, 1, 90000);
INSERT INTO manifests (repo_id, digest, media_type, size_bytes, pushed_at, min_layer_created_at, max_layer_created_at, next_validation_at) VALUES (1, 'sha256:65376fe20a63088d572f9dbcd2b11cc5408805d47d849d4d288278f5e56b0bd6', 'application/vnd.docker.distribution.manifest.list.v2+json', 4199557, 3600, 1, 1, 90000);

INSERT INTO quotas (auth_tenant_id, manifests) VALUES ('test1authtenant', 100);

INSERT INTO repos (id, account_name, name) VALUES (1, 'test1', 'foo');

INSERT INTO trivy_security_info (repo_id, digest, vuln_status, message, next_check_at, has_enriched_report) VALUES (1, 'sha256:01ec57127f7fa074c90f977d96254e80f18c10ef54e0cc5ca63e59c079458927', 'Clean', '', 3600, TRUE);
INSERT INTO trivy_security_info (repo_id, digest, vuln_status, message, next_check_at, has_enriched_report) VALUES (1, 'sha256:62756a4cfdd8acb07f8f5b530553e4cd1ac498c6920d6825dd64efc7573dacd7', 'Clean', '', 3600, TRUE);
INSERT INTO trivy_security_info (repo_id, digest, vuln_status, message, next_check_at) VALUES (1, 'sha256:65376fe20a63088d572f9dbcd2b11cc5408805d47d849d4d288278f5e56b0bd6', 'Pending', '', 3600);
