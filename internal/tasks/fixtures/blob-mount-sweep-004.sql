INSERT INTO accounts (name, auth_tenant_id) VALUES ('test1', 'test1authtenant');

INSERT INTO blob_mounts (blob_id, repo_id) VALUES (1, 1);
INSERT INTO blob_mounts (blob_id, repo_id) VALUES (2, 1);
INSERT INTO blob_mounts (blob_id, repo_id) VALUES (3, 1);
INSERT INTO blob_mounts (blob_id, repo_id) VALUES (5, 1);

INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, media_type, next_validation_at) VALUES (1, 'test1', 'sha256:fc0c9991b19af8980be450e0e582c6a31bbd62d3b0514cf0497b8330d4aab299', 1048681, '6b86b273ff34fce19d6b804eff5a3f5747ada4eaa22f1d49c01e52ddb7875b4b', 3600, 'application/vnd.docker.image.rootfs.diff.tar.gzip', 608400);
INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, media_type, next_validation_at) VALUES (2, 'test1', 'sha256:7e5e9e18c5d7f426f770339adab8d0d4682823dae447476e19fefef2b0c93610', 1048613, 'd4735e3a265e16eee03f59718b9b5d03019c07d8b6c51f90da3a666eec13ab35', 3600, 'application/vnd.docker.image.rootfs.diff.tar.zstd', 608400);
INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, media_type, next_validation_at) VALUES (3, 'test1', 'sha256:705a9ce670f317b4c06e40eb04063835586e0f6e182a3112827944782bcfdf77', 1412, '4e07408562bedb8b60ce05c1decfe3ad16b72230967de01f640b7e4729b49fce', 3600, 'application/vnd.docker.container.image.v1+json', 608400);
INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, next_validation_at) VALUES (4, 'test1', 'sha256:4ebfd1aafe77056e348c3ef48fa229b60390abb9c15c1023f609d88c06943eab', 1048576, '4b227777d4dd1fc61c6f884f48641d02b4d121d3fd328cb08b5531fcacdabf8a', 10800, 615600);
INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, next_validation_at) VALUES (5, 'test1', 'sha256:40d8c4c0cd13c2855f975aa1694a55634b990e506b0c55f75e560b35b4fdce3c', 1048613, 'ef2d127de37b942baad06145e54b0c619a1f22327b2ebbcfbec78f5564afe39d', 10800, 615600);

INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES (1, 'sha256:e5a38792414116c3f6cce116094be9901794137992490467548ba9b76b3c2ae6', 1);
INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES (1, 'sha256:e5a38792414116c3f6cce116094be9901794137992490467548ba9b76b3c2ae6', 2);
INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES (1, 'sha256:e5a38792414116c3f6cce116094be9901794137992490467548ba9b76b3c2ae6', 3);
INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES (1, 'sha256:e5a38792414116c3f6cce116094be9901794137992490467548ba9b76b3c2ae6', 5);

INSERT INTO manifest_contents (repo_id, digest, content) VALUES (1, 'sha256:e5a38792414116c3f6cce116094be9901794137992490467548ba9b76b3c2ae6', '{"config":{"digest":"sha256:705a9ce670f317b4c06e40eb04063835586e0f6e182a3112827944782bcfdf77","mediaType":"application/vnd.docker.container.image.v1+json","size":1412},"layers":[{"digest":"sha256:fc0c9991b19af8980be450e0e582c6a31bbd62d3b0514cf0497b8330d4aab299","mediaType":"application/vnd.docker.image.rootfs.diff.tar.gzip","size":1048681},{"digest":"sha256:7e5e9e18c5d7f426f770339adab8d0d4682823dae447476e19fefef2b0c93610","mediaType":"application/vnd.docker.image.rootfs.diff.tar.zstd","size":1048613}],"mediaType":"application/vnd.docker.distribution.manifest.v2+json","schemaVersion":2}');

INSERT INTO manifests (repo_id, digest, media_type, size_bytes, pushed_at, min_layer_created_at, max_layer_created_at, next_validation_at) VALUES (1, 'sha256:e5a38792414116c3f6cce116094be9901794137992490467548ba9b76b3c2ae6', 'application/vnd.docker.distribution.manifest.v2+json', 2099298, 3600, 1, 1, 90000);

INSERT INTO quotas (auth_tenant_id, manifests) VALUES ('test1authtenant', 100);

INSERT INTO repos (id, account_name, name, next_blob_mount_sweep_at) VALUES (1, 'test1', 'foo', 21600);

INSERT INTO trivy_security_info (repo_id, digest, vuln_status, message, next_check_at) VALUES (1, 'sha256:e5a38792414116c3f6cce116094be9901794137992490467548ba9b76b3c2ae6', 'Pending', '', 3600);
