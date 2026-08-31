INSERT INTO accounts (name, auth_tenant_id, upstream_peer_hostname, platform_filter, next_platform_filter_sync_at) VALUES ('test1', 'test1authtenant', 'registry.example.org', '[{"os":"linux","architecture":"amd64"}]', 3601);

INSERT INTO blob_mounts (blob_id, repo_id) VALUES (1, 1);
INSERT INTO blob_mounts (blob_id, repo_id) VALUES (2, 1);

INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, media_type, next_validation_at) VALUES (1, 'test1', 'sha256:6ad26aebb6f28b6d80617daf873c0c012dc60bea8d9c9d44d18342d1c6cca7fb', 1257, '6b86b273ff34fce19d6b804eff5a3f5747ada4eaa22f1d49c01e52ddb7875b4b', 2, 'application/vnd.docker.container.image.v1+json', 604802);
INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, media_type, next_validation_at) VALUES (2, 'test1', 'sha256:fc0c9991b19af8980be450e0e582c6a31bbd62d3b0514cf0497b8330d4aab299', 1048681, '', 0, 'application/vnd.docker.image.rootfs.diff.tar.gzip', 0);

INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES (1, 'sha256:e56badecae92c770230c2316af41f1f3018795f0624eb4dd22ade5179fe28c80', 1);
INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES (1, 'sha256:e56badecae92c770230c2316af41f1f3018795f0624eb4dd22ade5179fe28c80', 2);

INSERT INTO manifest_contents (repo_id, digest, content) VALUES (1, 'sha256:6437b15ce2a9577ad2a338003cde1d6be1fefe45f57a0f42ec929265f4b8c068', '{"manifests":[{"digest":"sha256:e56badecae92c770230c2316af41f1f3018795f0624eb4dd22ade5179fe28c80","mediaType":"application/vnd.docker.distribution.manifest.v2+json","platform":{"architecture":"amd64","os":"linux"},"size":428},{"digest":"sha256:746bd4616ac54afd9edad90eacd753bb74b72b5eee1a5d18ef4a7ba930c8d7d8","mediaType":"application/vnd.docker.distribution.manifest.v2+json","platform":{"architecture":"arm","os":"linux"},"size":428}],"mediaType":"application/vnd.docker.distribution.manifest.list.v2+json","schemaVersion":2}');
INSERT INTO manifest_contents (repo_id, digest, content) VALUES (1, 'sha256:e56badecae92c770230c2316af41f1f3018795f0624eb4dd22ade5179fe28c80', '{"config":{"digest":"sha256:6ad26aebb6f28b6d80617daf873c0c012dc60bea8d9c9d44d18342d1c6cca7fb","mediaType":"application/vnd.docker.container.image.v1+json","size":1257},"layers":[{"digest":"sha256:fc0c9991b19af8980be450e0e582c6a31bbd62d3b0514cf0497b8330d4aab299","mediaType":"application/vnd.docker.image.rootfs.diff.tar.gzip","size":1048681}],"mediaType":"application/vnd.docker.distribution.manifest.v2+json","schemaVersion":2}');

INSERT INTO manifest_manifest_refs (repo_id, parent_digest, child_digest) VALUES (1, 'sha256:6437b15ce2a9577ad2a338003cde1d6be1fefe45f57a0f42ec929265f4b8c068', 'sha256:e56badecae92c770230c2316af41f1f3018795f0624eb4dd22ade5179fe28c80');

INSERT INTO manifests (repo_id, digest, media_type, size_bytes, pushed_at, last_pulled_at, next_validation_at) VALUES (1, 'sha256:6437b15ce2a9577ad2a338003cde1d6be1fefe45f57a0f42ec929265f4b8c068', 'application/vnd.docker.distribution.manifest.list.v2+json', 1050893, 2, 2, 86402);
INSERT INTO manifests (repo_id, digest, media_type, size_bytes, pushed_at, next_validation_at) VALUES (1, 'sha256:e56badecae92c770230c2316af41f1f3018795f0624eb4dd22ade5179fe28c80', 'application/vnd.docker.distribution.manifest.v2+json', 1050366, 2, 86402);

INSERT INTO peers (hostname, our_password) VALUES ('registry.example.org', 'a4cb6fae5b8bb91b0b993486937103dab05eca93');

INSERT INTO quotas (auth_tenant_id, manifests) VALUES ('test1authtenant', 100);

INSERT INTO repos (id, account_name, name) VALUES (1, 'test1', 'foo');

INSERT INTO tags (repo_id, name, digest, pushed_at, last_pulled_at) VALUES (1, 'list', 'sha256:6437b15ce2a9577ad2a338003cde1d6be1fefe45f57a0f42ec929265f4b8c068', 2, 2);

INSERT INTO trivy_security_info (repo_id, digest, vuln_status, message, next_check_at) VALUES (1, 'sha256:6437b15ce2a9577ad2a338003cde1d6be1fefe45f57a0f42ec929265f4b8c068', 'Pending', '', 2);
INSERT INTO trivy_security_info (repo_id, digest, vuln_status, message, next_check_at) VALUES (1, 'sha256:e56badecae92c770230c2316af41f1f3018795f0624eb4dd22ade5179fe28c80', 'Pending', '', 2);
