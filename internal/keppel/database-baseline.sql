-- SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
-- SPDX-License-Identifier: Apache-2.0

CREATE TABLE accounts (
	name                            TEXT        NOT NULL PRIMARY KEY,
	auth_tenant_id                  TEXT        NOT NULL,
	upstream_peer_hostname          TEXT        NOT NULL DEFAULT '',
	next_blob_sweep_at              TIMESTAMPTZ DEFAULT NULL,
	next_storage_sweep_at           TIMESTAMPTZ DEFAULT NULL,
	next_federation_announcement_at TIMESTAMPTZ DEFAULT NULL,
	external_peer_url               TEXT        NOT NULL DEFAULT '',
	external_peer_username          TEXT        NOT NULL DEFAULT '',
	external_peer_password          TEXT        NOT NULL DEFAULT '',
	platform_filter                 TEXT        NOT NULL DEFAULT '',
	gc_policies_json                TEXT        NOT NULL DEFAULT '[]',
	security_scan_policies_json     TEXT        NOT NULL DEFAULT '[]',
	rbac_policies_json              TEXT        NOT NULL DEFAULT '[]',
	is_managed                      BOOLEAN     NOT NULL DEFAULT FALSE,
	next_enforcement_at             TIMESTAMPTZ DEFAULT NULL,
	is_deleting                     BOOLEAN NOT NULL DEFAULT FALSE,
	next_deletion_attempt_at        TIMESTAMPTZ DEFAULT NULL,
	tag_policies_json               TEXT        NOT NULL DEFAULT '[]',
	rule_for_manifest               TEXT        NOT NULL DEFAULT '',
	next_platform_filter_sync_at    TIMESTAMPTZ DEFAULT NULL,
	anon_rbac_policies_json         TEXT        NOT NULL DEFAULT '[]',
	CONSTRAINT platform_filter_sync_on_replicas CHECK ((upstream_peer_hostname = '') = (next_platform_filter_sync_at IS NULL)),
	CONSTRAINT anon_rbac_policies_not_empty     CHECK (anon_rbac_policies_json != ''),
	CONSTRAINT rbac_policies_not_empty          CHECK (rbac_policies_json != ''),
	CONSTRAINT gc_policies_not_empty            CHECK (gc_policies_json != ''),
	CONSTRAINT tag_policies_not_empty           CHECK (tag_policies_json != ''),
	CONSTRAINT security_scan_policies_not_empty CHECK (security_scan_policies_json != '')
);

CREATE TABLE quotas (
	auth_tenant_id TEXT   NOT NULL PRIMARY KEY,
	manifests      BIGINT NOT NULL,
	bytes          BIGINT NOT NULL DEFAULT -1
);

CREATE TABLE peers (
	hostname                     TEXT        NOT NULL PRIMARY KEY,
	our_password                 TEXT        NOT NULL DEFAULT '',
	their_current_password_hash  TEXT        NOT NULL DEFAULT '',
	their_previous_password_hash TEXT        NOT NULL DEFAULT '',
	last_peered_at               TIMESTAMPTZ DEFAULT NULL,
	use_for_pull_delegation      BOOLEAN     NOT NULL DEFAULT TRUE
);

CREATE TABLE repos (
	id                       BIGSERIAL   NOT NULL PRIMARY KEY,
	account_name             TEXT        NOT NULL REFERENCES accounts ON DELETE CASCADE,
	name                     TEXT        NOT NULL,
	next_blob_mount_sweep_at TIMESTAMPTZ DEFAULT NULL,
	next_manifest_sync_at    TIMESTAMPTZ DEFAULT NULL,
	next_gc_at               TIMESTAMPTZ DEFAULT NULL,
	UNIQUE (account_name, name)
);

CREATE TABLE blobs (
	id                       BIGSERIAL   NOT NULL PRIMARY KEY,
	account_name             TEXT        NOT NULL REFERENCES accounts ON DELETE CASCADE,
	digest                   TEXT        NOT NULL,
	size_bytes               BIGINT      NOT NULL,
	storage_id               TEXT        NOT NULL,
	pushed_at                TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	validation_error_message TEXT        NOT NULL DEFAULT '',
	can_be_deleted_at        TIMESTAMPTZ DEFAULT NULL,
	media_type               TEXT        NOT NULL DEFAULT '',
	blocks_vuln_scanning     TEXT        DEFAULT NULL,
	next_validation_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	UNIQUE (account_name, digest)
);
CREATE INDEX ON blobs (next_validation_at); -- used by BlobValidationJob

CREATE TABLE blob_mounts (
	blob_id                BIGINT      NOT NULL REFERENCES blobs ON DELETE CASCADE,
	repo_id                BIGINT      NOT NULL REFERENCES repos ON DELETE CASCADE,
	can_be_deleted_at      TIMESTAMPTZ DEFAULT NULL,
	UNIQUE (blob_id, repo_id)
);
CREATE INDEX ON blob_mounts (can_be_deleted_at NULLS FIRST, repo_id); -- used by BlobMountSweepJob

CREATE TABLE uploads (
	repo_id     BIGINT      NOT NULL REFERENCES repos ON DELETE CASCADE,
	uuid        TEXT        NOT NULL,
	storage_id  TEXT        NOT NULL,
	size_bytes  BIGINT      NOT NULL,
	digest      TEXT        NOT NULL,
	num_chunks  INT         NOT NULL,
	updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	PRIMARY KEY (repo_id, uuid)
);

CREATE TABLE manifests (
	repo_id                  BIGINT      NOT NULL REFERENCES repos ON DELETE CASCADE,
	digest                   TEXT        NOT NULL,
	media_type               TEXT        NOT NULL,
	size_bytes               BIGINT      NOT NULL,
	pushed_at                TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	validation_error_message TEXT        NOT NULL DEFAULT '',
	last_pulled_at           TIMESTAMPTZ DEFAULT NULL,
	labels_json              TEXT        NOT NULL DEFAULT '',
	gc_status_json           TEXT        NOT NULL DEFAULT '',
	min_layer_created_at     TIMESTAMPTZ DEFAULT NULL,
	max_layer_created_at     TIMESTAMPTZ DEFAULT NULL,
	next_validation_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	annotations_json         TEXT NOT NULL DEFAULT '',
	artifact_type            TEXT NOT NULL DEFAULT '',
	subject_digest           TEXT NOT NULL DEFAULT '',
	PRIMARY KEY (repo_id, digest)
);
CREATE INDEX ON manifests (next_validation_at); -- used by ManifestValidationJob
CREATE INDEX ON manifests (validation_error_message) WHERE validation_error_message != '';
CREATE INDEX ON manifests (repo_id, subject_digest) WHERE subject_digest != '';

CREATE TABLE manifest_contents (
	repo_id BIGINT NOT NULL,
	digest  TEXT   NOT NULL,
	content BYTEA  NOT NULL,
	FOREIGN KEY (repo_id, digest) REFERENCES manifests ON DELETE CASCADE,
	UNIQUE (repo_id, digest)
);

CREATE TABLE manifest_blob_refs (
	repo_id BIGINT NOT NULL,
	digest  TEXT   NOT NULL,
	blob_id BIGINT NOT NULL,
	FOREIGN KEY (repo_id, digest) REFERENCES manifests ON DELETE CASCADE,
	FOREIGN KEY (blob_id, repo_id) REFERENCES blob_mounts (blob_id, repo_id) ON DELETE RESTRICT,
	UNIQUE (repo_id, digest, blob_id)
);

CREATE TABLE manifest_manifest_refs (
	repo_id       BIGINT NOT NULL,
	parent_digest TEXT   NOT NULL,
	child_digest  TEXT   NOT NULL,
	FOREIGN KEY (repo_id, parent_digest) REFERENCES manifests (repo_id, digest) ON DELETE CASCADE,
	FOREIGN KEY (repo_id, child_digest)  REFERENCES manifests (repo_id, digest) ON DELETE RESTRICT,
	UNIQUE (repo_id, parent_digest, child_digest)
);

CREATE TABLE tags (
	repo_id        BIGINT      NOT NULL REFERENCES repos ON DELETE CASCADE,
	name           TEXT        NOT NULL,
	digest         TEXT        NOT NULL,
	pushed_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	last_pulled_at TIMESTAMPTZ DEFAULT NULL,
	PRIMARY KEY (repo_id, name),
	FOREIGN KEY (repo_id, digest) REFERENCES manifests ON DELETE CASCADE
);

CREATE TABLE trivy_security_info (
	repo_id                BIGINT      NOT NULL REFERENCES repos ON DELETE CASCADE,
	digest                 TEXT        NOT NULL,
	vuln_status            TEXT        NOT NULL,
	message                TEXT        NOT NULL,
	next_check_at          TIMESTAMPTZ DEFAULT NULL,
	checked_at             TIMESTAMPTZ DEFAULT NULL, -- NULL before first check
	check_duration_secs    REAL        DEFAULT NULL, -- NULL before first check
	has_enriched_report    BOOLEAN     NOT NULL DEFAULT FALSE,
	vuln_status_changed_at TIMESTAMPTZ DEFAULT NULL,
	FOREIGN KEY (repo_id, digest) REFERENCES manifests ON DELETE CASCADE,
	UNIQUE (repo_id, digest),
	CONSTRAINT next_check_at_only_null_when_rotten CHECK ((vuln_status = 'Rotten') = (next_check_at IS NULL))
);
CREATE INDEX ON trivy_security_info (repo_id, digest) WHERE vuln_status <> 'Clean';

CREATE TABLE pending_blobs (
	account_name TEXT        NOT NULL REFERENCES accounts ON DELETE CASCADE,
	digest       TEXT        NOT NULL,
	reason       TEXT        NOT NULL,
	since        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	PRIMARY KEY (account_name, digest)
);

CREATE TABLE unknown_blobs (
	account_name      TEXT        NOT NULL REFERENCES accounts ON DELETE CASCADE,
	storage_id        TEXT        NOT NULL,
	can_be_deleted_at TIMESTAMPTZ NOT NULL,
	PRIMARY KEY (account_name, storage_id)
);

CREATE TABLE unknown_manifests (
	account_name      TEXT        NOT NULL REFERENCES accounts ON DELETE CASCADE,
	repo_name         TEXT        NOT NULL,
	digest            TEXT        NOT NULL,
	can_be_deleted_at TIMESTAMPTZ NOT NULL,
	PRIMARY KEY (account_name, repo_name, digest)
);

CREATE TABLE unknown_trivy_reports (
	account_name      TEXT        NOT NULL REFERENCES accounts ON DELETE CASCADE,
	repo_name         TEXT        NOT NULL,
	digest            TEXT        NOT NULL,
	format            TEXT        NOT NULL,
	can_be_deleted_at TIMESTAMPTZ NOT NULL,
	PRIMARY KEY (account_name, repo_name, digest, format)
);
