INSERT INTO accounts (name, auth_tenant_id) VALUES ('test1', 'tenant1');
INSERT INTO accounts (name, auth_tenant_id) VALUES ('test2', 'tenant2');

INSERT INTO blob_mounts (blob_id, repo_id) VALUES (1, 5);
INSERT INTO blob_mounts (blob_id, repo_id) VALUES (10, 5);
INSERT INTO blob_mounts (blob_id, repo_id) VALUES (11, 5);
INSERT INTO blob_mounts (blob_id, repo_id) VALUES (2, 5);
INSERT INTO blob_mounts (blob_id, repo_id) VALUES (3, 5);
INSERT INTO blob_mounts (blob_id, repo_id) VALUES (4, 5);
INSERT INTO blob_mounts (blob_id, repo_id) VALUES (5, 5);
INSERT INTO blob_mounts (blob_id, repo_id) VALUES (6, 5);
INSERT INTO blob_mounts (blob_id, repo_id) VALUES (7, 5);
INSERT INTO blob_mounts (blob_id, repo_id) VALUES (8, 5);
INSERT INTO blob_mounts (blob_id, repo_id) VALUES (9, 5);

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

INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES (5, 'sha256:846dc180d9b05c084ba40cfcbf833c055041f2a48d6ef2e5e9b73baf590fea03', 11);

INSERT INTO manifest_contents (repo_id, digest, content) VALUES (5, 'sha256:846dc180d9b05c084ba40cfcbf833c055041f2a48d6ef2e5e9b73baf590fea03', '{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.image.manifest.v1+json","digest":"sha256:74234e98afe7498fb5daf1f36ac2d78acc339464f950703b8c019892f982b90b","size":4},"layers":[],"subject":{"mediaType":"application/vnd.oci.image.manifest.v1+json","digest":"sha256:040a5a009f9b9d5e4771742174142e74fa2d3e0aaa3df5717f01ade338d75d0e","size":0}}');

INSERT INTO manifests (repo_id, digest, media_type, size_bytes, pushed_at, next_validation_at) VALUES (5, 'sha256:040a5a009f9b9d5e4771742174142e74fa2d3e0aaa3df5717f01ade338d75d0e', '', 9000, 10090, 96490);
INSERT INTO manifests (repo_id, digest, media_type, size_bytes, pushed_at, next_validation_at) VALUES (5, 'sha256:04abc8821a06e5a30937967d11ad10221cb5ac3b5273e434f1284ee87129a061', '', 8000, 10080, 96480);
INSERT INTO manifests (repo_id, digest, media_type, size_bytes, pushed_at, next_validation_at) VALUES (5, 'sha256:27ecd0a598e76f8a2fd264d427df0a119903e8eae384e478902541756f089dd1', '', 4000, 10040, 96440);
INSERT INTO manifests (repo_id, digest, media_type, size_bytes, pushed_at, next_validation_at) VALUES (5, 'sha256:377a23f52c6b357696238c3318f677a082dd3430bb6691042bd550a5cda28ebb', '', 5000, 10050, 96450);
INSERT INTO manifests (repo_id, digest, media_type, size_bytes, pushed_at, next_validation_at) VALUES (5, 'sha256:4bf5122f344554c53bde2ebb8cd2b7e3d1600ad631c385a5d7cce23c7785459a', '', 1000, 10010, 96410);
INSERT INTO manifests (repo_id, digest, media_type, size_bytes, pushed_at, next_validation_at) VALUES (5, 'sha256:75c8fd04ad916aec3e3d5cb76a452b116b3d4d0912a0a485e9fb8e3d240e210c', '', 3000, 10030, 96430);
INSERT INTO manifests (repo_id, digest, media_type, size_bytes, pushed_at, next_validation_at, artifact_type, subject_digest) VALUES (5, 'sha256:846dc180d9b05c084ba40cfcbf833c055041f2a48d6ef2e5e9b73baf590fea03', 'application/vnd.oci.image.manifest.v1+json', 413, 0, 86400, 'application/vnd.oci.image.manifest.v1+json', 'sha256:040a5a009f9b9d5e4771742174142e74fa2d3e0aaa3df5717f01ade338d75d0e');
INSERT INTO manifests (repo_id, digest, media_type, size_bytes, pushed_at, next_validation_at) VALUES (5, 'sha256:9dcf97a184f32623d11a73124ceb99a5709b083721e878a16d78f596718ba7b2', '', 2000, 10020, 96420);
INSERT INTO manifests (repo_id, digest, media_type, size_bytes, pushed_at, next_validation_at) VALUES (5, 'sha256:c36336f242c655c52fa06c4d03f665ca9ea0bb84f20f1b1f90976aa58ca40a4a', '', 7000, 10070, 96470);
INSERT INTO manifests (repo_id, digest, media_type, size_bytes, pushed_at, next_validation_at) VALUES (5, 'sha256:dfea2964b5deedea7b1ef077de529c3959e6788bdbb3441e70c77a1ae875bb48', '', 6000, 10060, 96460);

INSERT INTO quotas (auth_tenant_id, manifests) VALUES ('tenant1', 100);
INSERT INTO quotas (auth_tenant_id, manifests) VALUES ('tenant2', 100);

INSERT INTO repos (id, account_name, name) VALUES (1, 'test1', 'repo1-1');
INSERT INTO repos (id, account_name, name) VALUES (10, 'test2', 'repo2-5');
INSERT INTO repos (id, account_name, name) VALUES (2, 'test2', 'repo2-1');
INSERT INTO repos (id, account_name, name) VALUES (3, 'test1', 'repo1-2');
INSERT INTO repos (id, account_name, name) VALUES (4, 'test2', 'repo2-2');
INSERT INTO repos (id, account_name, name) VALUES (5, 'test1', 'repo1-3');
INSERT INTO repos (id, account_name, name) VALUES (6, 'test2', 'repo2-3');
INSERT INTO repos (id, account_name, name) VALUES (7, 'test1', 'repo1-4');
INSERT INTO repos (id, account_name, name) VALUES (8, 'test2', 'repo2-4');
INSERT INTO repos (id, account_name, name) VALUES (9, 'test1', 'repo1-5');

INSERT INTO tags (repo_id, name, digest, pushed_at) VALUES (5, 'sha256-040a5a009f9b9d5e4771742174142e74fa2d3e0aaa3df5717f01ade338d75d0e', 'sha256:846dc180d9b05c084ba40cfcbf833c055041f2a48d6ef2e5e9b73baf590fea03', 0);
INSERT INTO tags (repo_id, name, digest, pushed_at) VALUES (5, 'tag1', 'sha256:4bf5122f344554c53bde2ebb8cd2b7e3d1600ad631c385a5d7cce23c7785459a', 20010);
INSERT INTO tags (repo_id, name, digest, pushed_at) VALUES (5, 'tag2', 'sha256:9dcf97a184f32623d11a73124ceb99a5709b083721e878a16d78f596718ba7b2', 20020);
INSERT INTO tags (repo_id, name, digest, pushed_at) VALUES (5, 'tag3', 'sha256:75c8fd04ad916aec3e3d5cb76a452b116b3d4d0912a0a485e9fb8e3d240e210c', 20030);

INSERT INTO trivy_security_info (repo_id, digest, vuln_status, message, next_check_at) VALUES (5, 'sha256:040a5a009f9b9d5e4771742174142e74fa2d3e0aaa3df5717f01ade338d75d0e', 'Pending', '', 0);
INSERT INTO trivy_security_info (repo_id, digest, vuln_status, message, next_check_at) VALUES (5, 'sha256:04abc8821a06e5a30937967d11ad10221cb5ac3b5273e434f1284ee87129a061', 'Pending', '', 0);
INSERT INTO trivy_security_info (repo_id, digest, vuln_status, message, next_check_at) VALUES (5, 'sha256:27ecd0a598e76f8a2fd264d427df0a119903e8eae384e478902541756f089dd1', 'Pending', '', 0);
INSERT INTO trivy_security_info (repo_id, digest, vuln_status, message, next_check_at) VALUES (5, 'sha256:377a23f52c6b357696238c3318f677a082dd3430bb6691042bd550a5cda28ebb', 'Pending', '', 0);
INSERT INTO trivy_security_info (repo_id, digest, vuln_status, message, next_check_at) VALUES (5, 'sha256:4bf5122f344554c53bde2ebb8cd2b7e3d1600ad631c385a5d7cce23c7785459a', 'Pending', '', 0);
INSERT INTO trivy_security_info (repo_id, digest, vuln_status, message, next_check_at) VALUES (5, 'sha256:75c8fd04ad916aec3e3d5cb76a452b116b3d4d0912a0a485e9fb8e3d240e210c', 'Pending', '', 0);
INSERT INTO trivy_security_info (repo_id, digest, vuln_status, message, next_check_at) VALUES (5, 'sha256:846dc180d9b05c084ba40cfcbf833c055041f2a48d6ef2e5e9b73baf590fea03', 'Pending', '', 0);
INSERT INTO trivy_security_info (repo_id, digest, vuln_status, message, next_check_at) VALUES (5, 'sha256:9dcf97a184f32623d11a73124ceb99a5709b083721e878a16d78f596718ba7b2', 'Pending', '', 0);
INSERT INTO trivy_security_info (repo_id, digest, vuln_status, message, next_check_at) VALUES (5, 'sha256:c36336f242c655c52fa06c4d03f665ca9ea0bb84f20f1b1f90976aa58ca40a4a', 'Pending', '', 0);
INSERT INTO trivy_security_info (repo_id, digest, vuln_status, message, next_check_at) VALUES (5, 'sha256:dfea2964b5deedea7b1ef077de529c3959e6788bdbb3441e70c77a1ae875bb48', 'Pending', '', 0);
