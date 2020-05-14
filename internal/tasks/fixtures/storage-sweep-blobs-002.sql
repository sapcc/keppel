INSERT INTO accounts (name, auth_tenant_id, upstream_peer_hostname, required_labels, metadata_json, next_blob_sweep_at, next_storage_sweep_at, next_federation_announcement_at, in_maintenance) VALUES ('test1', 'test1authtenant', '', '', '', NULL, 86400, NULL, FALSE);

INSERT INTO blob_mounts (blob_id, repo_id, can_be_deleted_at) VALUES (1, 1, NULL);
INSERT INTO blob_mounts (blob_id, repo_id, can_be_deleted_at) VALUES (2, 1, NULL);
INSERT INTO blob_mounts (blob_id, repo_id, can_be_deleted_at) VALUES (3, 1, NULL);
INSERT INTO blob_mounts (blob_id, repo_id, can_be_deleted_at) VALUES (4, 1, NULL);
INSERT INTO blob_mounts (blob_id, repo_id, can_be_deleted_at) VALUES (5, 1, NULL);
INSERT INTO blob_mounts (blob_id, repo_id, can_be_deleted_at) VALUES (6, 1, NULL);

INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, validated_at, validation_error_message, can_be_deleted_at) VALUES (1, 'test1', 'sha256:a718f4a112454b50c8ecd2b0a5b00eb32ee90699593625139cd3fabc97dcce8d', 1048576, 'a718f4a112454b50c8ecd2b0a5b00eb32ee90699593625139cd3fabc97dcce8d', 3600, 3600, '', NULL);
INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, validated_at, validation_error_message, can_be_deleted_at) VALUES (2, 'test1', 'sha256:d87b0830e55e19fd0825bebaa110ebade6de7d1fcf2ddf0fbd5762e1f809370e', 1048576, 'd87b0830e55e19fd0825bebaa110ebade6de7d1fcf2ddf0fbd5762e1f809370e', 3600, 3600, '', NULL);
INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, validated_at, validation_error_message, can_be_deleted_at) VALUES (3, 'test1', 'sha256:8744de6601dad881db9d58714824690eff63aed8033aaf025a90b8a5c8114099', 1412, '8744de6601dad881db9d58714824690eff63aed8033aaf025a90b8a5c8114099', 3600, 3600, '', NULL);
INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, validated_at, validation_error_message, can_be_deleted_at) VALUES (4, 'test1', 'sha256:aa9bf3251d0754379f51f5a7ca15835c504a1b1e8fa6663463c22dfc9ae527b8', 1048576, 'aa9bf3251d0754379f51f5a7ca15835c504a1b1e8fa6663463c22dfc9ae527b8', 3600, 3600, '', NULL);
INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, validated_at, validation_error_message, can_be_deleted_at) VALUES (5, 'test1', 'sha256:5dfe56df10488d9c1f75414293fe474eaa7fca63acdee374bc371250f1a372b4', 1048576, '5dfe56df10488d9c1f75414293fe474eaa7fca63acdee374bc371250f1a372b4', 3600, 3600, '', NULL);
INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, validated_at, validation_error_message, can_be_deleted_at) VALUES (6, 'test1', 'sha256:db8bc83bac14839cc7d46e346e417a9ecd115732205c79311c5e191dd156ed18', 1412, 'db8bc83bac14839cc7d46e346e417a9ecd115732205c79311c5e191dd156ed18', 3600, 3600, '', NULL);
INSERT INTO blobs (id, account_name, digest, size_bytes, storage_id, pushed_at, validated_at, validation_error_message, can_be_deleted_at) VALUES (7, 'test1', 'sha256:8ce2cde98e42a9004a44265af9c1b03aed84e49e4adaa9fc3032a568ebdc6322', 1048576, '8ce2cde98e42a9004a44265af9c1b03aed84e49e4adaa9fc3032a568ebdc6322', 36000, 36000, '', NULL);

INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES (1, 'sha256:33ef30d47bd666b28f971cc3f07b00aca72d55865e79d6ca03a836647bb81f42', 4);
INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES (1, 'sha256:33ef30d47bd666b28f971cc3f07b00aca72d55865e79d6ca03a836647bb81f42', 5);
INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES (1, 'sha256:33ef30d47bd666b28f971cc3f07b00aca72d55865e79d6ca03a836647bb81f42', 6);
INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES (1, 'sha256:7ce8d2ddbc66e475563019803ff254fb78b7becafd39959dc735ace4efaf395e', 1);
INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES (1, 'sha256:7ce8d2ddbc66e475563019803ff254fb78b7becafd39959dc735ace4efaf395e', 2);
INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES (1, 'sha256:7ce8d2ddbc66e475563019803ff254fb78b7becafd39959dc735ace4efaf395e', 3);

INSERT INTO manifest_manifest_refs (repo_id, parent_digest, child_digest) VALUES (1, 'sha256:53f2e51cfb1ab43831ba433a7be298d30fd2555bee9d6765117148a19a6d28ec', 'sha256:33ef30d47bd666b28f971cc3f07b00aca72d55865e79d6ca03a836647bb81f42');
INSERT INTO manifest_manifest_refs (repo_id, parent_digest, child_digest) VALUES (1, 'sha256:53f2e51cfb1ab43831ba433a7be298d30fd2555bee9d6765117148a19a6d28ec', 'sha256:7ce8d2ddbc66e475563019803ff254fb78b7becafd39959dc735ace4efaf395e');

INSERT INTO manifests (repo_id, digest, media_type, size_bytes, pushed_at, validated_at, validation_error_message, last_pulled_at) VALUES (1, 'sha256:33ef30d47bd666b28f971cc3f07b00aca72d55865e79d6ca03a836647bb81f42', 'application/vnd.docker.distribution.manifest.v2+json', 2099156, 3600, 3600, '', NULL);
INSERT INTO manifests (repo_id, digest, media_type, size_bytes, pushed_at, validated_at, validation_error_message, last_pulled_at) VALUES (1, 'sha256:53f2e51cfb1ab43831ba433a7be298d30fd2555bee9d6765117148a19a6d28ec', 'application/vnd.docker.distribution.manifest.list.v2+json', 1715, 3600, 3600, '', NULL);
INSERT INTO manifests (repo_id, digest, media_type, size_bytes, pushed_at, validated_at, validation_error_message, last_pulled_at) VALUES (1, 'sha256:7ce8d2ddbc66e475563019803ff254fb78b7becafd39959dc735ace4efaf395e', 'application/vnd.docker.distribution.manifest.v2+json', 2099156, 3600, 3600, '', NULL);

INSERT INTO repos (id, account_name, name, next_blob_mount_sweep_at, next_manifest_sync_at) VALUES (1, 'test1', 'foo', NULL, NULL);

INSERT INTO uploads (repo_id, uuid, storage_id, size_bytes, digest, num_chunks, updated_at) VALUES (1, 'a29d525c-2273-44ba-83a8-eafd447f1cb8', '6ea2d07e23765eb41a6577d42905eccf55b6697cb9dd2a544333766b65748ec5', 1048576, 'sha256:6ea2d07e23765eb41a6577d42905eccf55b6697cb9dd2a544333766b65748ec5', 1, 3600);
