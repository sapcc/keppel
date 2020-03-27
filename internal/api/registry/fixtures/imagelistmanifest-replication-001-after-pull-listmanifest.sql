INSERT INTO accounts (name, auth_tenant_id, upstream_peer_hostname, required_labels, blobs_sweeped_at, storage_sweeped_at, metadata_json) VALUES ('test1', 'test1authtenant', 'registry.example.org', '', NULL, NULL, '');

INSERT INTO blob_mounts (blob_id, repo_id, marked_for_deletion_at) VALUES (1, 1, NULL);
INSERT INTO blob_mounts (blob_id, repo_id, marked_for_deletion_at) VALUES (2, 1, NULL);
INSERT INTO blob_mounts (blob_id, repo_id, marked_for_deletion_at) VALUES (3, 1, NULL);
INSERT INTO blob_mounts (blob_id, repo_id, marked_for_deletion_at) VALUES (4, 1, NULL);

INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, validated_at, validation_error_message, marked_for_deletion_at) VALUES (1, 'test1', 'sha256:359aa5408fc03ed0a8c865fcf4e0a04d086c4a2ba202406990afcc5efb05c3d7', 1257, '6b86b273ff34fce19d6b804eff5a3f5747ada4eaa22f1d49c01e52ddb7875b4b', 2, 2, '', NULL);
INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, validated_at, validation_error_message, marked_for_deletion_at) VALUES (2, 'test1', 'sha256:a718f4a112454b50c8ecd2b0a5b00eb32ee90699593625139cd3fabc97dcce8d', 1048576, '', 0, 0, '', NULL);
INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, validated_at, validation_error_message, marked_for_deletion_at) VALUES (3, 'test1', 'sha256:dcdce891e29926a3fc127ed32938d9de2aad031b428130f6afcfa45db7ad8564', 1257, 'd4735e3a265e16eee03f59718b9b5d03019c07d8b6c51f90da3a666eec13ab35', 2, 2, '', NULL);
INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, validated_at, validation_error_message, marked_for_deletion_at) VALUES (4, 'test1', 'sha256:d87b0830e55e19fd0825bebaa110ebade6de7d1fcf2ddf0fbd5762e1f809370e', 1048576, '', 0, 0, '', NULL);

INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES (1, 'sha256:a1efa53bd4bbcc4878997c775688438b8ccfd29ccf71f110296dc62d5dabc42d', 3);
INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES (1, 'sha256:a1efa53bd4bbcc4878997c775688438b8ccfd29ccf71f110296dc62d5dabc42d', 4);
INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES (1, 'sha256:be414f354c95cb5c3e26d604f5fc79523c68c3f86e0fae98060d5bbc8db466c3', 1);
INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES (1, 'sha256:be414f354c95cb5c3e26d604f5fc79523c68c3f86e0fae98060d5bbc8db466c3', 2);

INSERT INTO manifest_manifest_refs (repo_id, parent_digest, child_digest) VALUES (1, 'sha256:cbf7f5d81c97e4b501d936b0323a39a4972b44108d301d72b3b9b76e92886a22', 'sha256:a1efa53bd4bbcc4878997c775688438b8ccfd29ccf71f110296dc62d5dabc42d');
INSERT INTO manifest_manifest_refs (repo_id, parent_digest, child_digest) VALUES (1, 'sha256:cbf7f5d81c97e4b501d936b0323a39a4972b44108d301d72b3b9b76e92886a22', 'sha256:be414f354c95cb5c3e26d604f5fc79523c68c3f86e0fae98060d5bbc8db466c3');

INSERT INTO manifests (repo_id, digest, media_type, size_bytes, pushed_at, validated_at, validation_error_message) VALUES (1, 'sha256:a1efa53bd4bbcc4878997c775688438b8ccfd29ccf71f110296dc62d5dabc42d', 'application/vnd.docker.distribution.manifest.v2+json', 1050261, 2, 2, '');
INSERT INTO manifests (repo_id, digest, media_type, size_bytes, pushed_at, validated_at, validation_error_message) VALUES (1, 'sha256:be414f354c95cb5c3e26d604f5fc79523c68c3f86e0fae98060d5bbc8db466c3', 'application/vnd.docker.distribution.manifest.v2+json', 1050261, 2, 2, '');
INSERT INTO manifests (repo_id, digest, media_type, size_bytes, pushed_at, validated_at, validation_error_message) VALUES (1, 'sha256:cbf7f5d81c97e4b501d936b0323a39a4972b44108d301d72b3b9b76e92886a22', 'application/vnd.docker.distribution.manifest.list.v2+json', 1387, 2, 2, '');

INSERT INTO peers (hostname, our_password, their_current_password_hash, their_previous_password_hash, last_peered_at) VALUES ('registry.example.org', 'a4cb6fae5b8bb91b0b993486937103dab05eca93', '', '', NULL);

INSERT INTO quotas (auth_tenant_id, manifests) VALUES ('test1authtenant', 100);

INSERT INTO repos (id, account_name, name, blob_mounts_sweeped_at, manifests_synced_at) VALUES (1, 'test1', 'foo', NULL, NULL);

INSERT INTO tags (repo_id, name, digest, pushed_at) VALUES (1, 'list', 'sha256:cbf7f5d81c97e4b501d936b0323a39a4972b44108d301d72b3b9b76e92886a22', 2);
