// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package testcmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	. "github.com/majewsky/gg/option"
	"github.com/opencontainers/go-digest"
	"github.com/sapcc/go-bits/logg"
	"github.com/spf13/cobra"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
	"github.com/sapcc/keppel/internal/trivy"
)

var (
	accountName         string
	accountAuthTenantID string
	filesystemPath      string
)

// AddCommandTo mounts this command into the command hierarchy.
func AddCommandTo(parent *cobra.Command) {
	testCmd := &cobra.Command{
		Use:     "test <storage-driver> <method> <args...>",
		Example: "  keppel test swift read-manifest repo sha256:abc123 --account-name my-account",
		Short:   "Manual test harness for storage driver implementations.",
		Long: `Manual test harness for storage driver implementations.
Performs the minimum required setup to obtain the respective storage driver instance, executes the method and then displays the result.

Available storage drivers:
  swift                    OpenStack Swift storage driver
  filesystem               Local filesystem storage driver
  in-memory-for-testing    In-memory storage driver for testing

Available methods:
  Blob operations:
    append-to-blob         <storage-id> <chunk-number> <data>
    finalize-blob          <storage-id> <chunk-count>
    abort-blob-upload      <storage-id> <chunk-count>
    read-blob              <storage-id>
    url-for-blob           <storage-id>
    delete-blob            <storage-id>

  Manifest operations:
    read-manifest          <repo-name> <digest>
    write-manifest         <repo-name> <digest> <content>
    delete-manifest        <repo-name> <digest>

  Trivy report operations:
    read-trivy-report      <repo-name> <digest> <format>
    write-trivy-report     <repo-name> <digest> <payload-json>
    delete-trivy-report    <repo-name> <digest> <format>

  Storage operations:
    list-storage-contents  (no arguments)

  Account operations:
    can-setup-account      (no arguments)
    cleanup-account        (no arguments)`,
		Args: cobra.MinimumNArgs(2),
		Run:  run,
	}

	testCmd.Flags().StringVarP(&accountName, "account-name", "a", "", "Account name for Swift storage driver (required when using Swift)")
	testCmd.Flags().StringVarP(&accountAuthTenantID, "account-auth-tenant-id", "", "", "Account auth tenant ID for Swift storage driver (required when using Swift)")
	testCmd.Flags().StringVarP(&filesystemPath, "path", "p", "/tmp/keppel-test-storage", "Storage path for filesystem driver")

	parent.AddCommand(testCmd)
}

func run(cmd *cobra.Command, args []string) {
	driverType := args[0]
	method := args[1]
	methodArgs := args[2:]

	cfg := keppel.Configuration{}

	account := models.ReducedAccount{
		Name:         models.AccountName("test-account"),
		AuthTenantID: "test-tenant-id",
	}
	authConfig := `{"type":"trivial","params":{"username":"test","password":"test"}}`

	var storageConfig string
	switch driverType {
	case "in-memory-for-testing":
		storageConfig = `{"type":"in-memory-for-testing"}`
	case "filesystem":
		storageConfig = fmt.Sprintf(`{"type":"filesystem","params":{"path":"%s"}}`, filesystemPath)
	case "swift":
		if accountName == "" {
			logg.Fatal("--account-name flag is required when using Swift storage driver")
		}

		if accountAuthTenantID == "" {
			logg.Fatal("--account-auth-tenant-id flag is required when using Swift storage driver")
		}

		account = models.ReducedAccount{
			Name:         models.AccountName(accountName),
			AuthTenantID: accountAuthTenantID,
		}
		authConfig = `{"type":"keystone","params":{"oslo_policy_path":"cmd/test/policy.json"}}`
		storageConfig = `{"type":"swift"}`
	default:
		logg.Fatal("unknown storage driver: %s. Supported drivers: swift, filesystem, in-memory-for-testing", driverType)
	}

	ad, err := keppel.NewAuthDriver(cmd.Context(), authConfig, nil)
	if err != nil {
		logg.Fatal("failed to initialize auth driver: %s", err.Error())
	}

	sd, err := keppel.NewStorageDriver(storageConfig, ad, cfg)
	if err != nil {
		logg.Fatal("failed to initialize storage driver: %s", err.Error())
	}

	switch method {
	case "append-to-blob":
		executeAppendToBlob(cmd.Context(), sd, account, methodArgs)
	case "finalize-blob":
		executeFinalizeBlob(cmd.Context(), sd, account, methodArgs)
	case "abort-blob-upload":
		executeAbortBlobUpload(cmd.Context(), sd, account, methodArgs)
	case "read-blob":
		executeReadBlob(cmd.Context(), sd, account, methodArgs)
	case "url-for-blob":
		executeURLForBlob(cmd.Context(), sd, account, methodArgs)
	case "delete-blob":
		executeDeleteBlob(cmd.Context(), sd, account, methodArgs)
	case "read-manifest":
		executeReadManifest(cmd.Context(), sd, account, methodArgs)
	case "write-manifest":
		executeWriteManifest(cmd.Context(), sd, account, methodArgs)
	case "delete-manifest":
		executeDeleteManifest(cmd.Context(), sd, account, methodArgs)
	case "read-trivy-report":
		executeReadTrivyReport(cmd.Context(), sd, account, methodArgs)
	case "write-trivy-report":
		executeWriteTrivyReport(cmd.Context(), sd, account, methodArgs)
	case "delete-trivy-report":
		executeDeleteTrivyReport(cmd.Context(), sd, account, methodArgs)
	case "list-storage-contents":
		executeListStorageContents(cmd.Context(), sd, account)
	case "can-setup-account":
		executeCanSetupAccount(cmd.Context(), sd, account)
	case "cleanup-account":
		executeCleanupAccount(cmd.Context(), sd, account)
	default:
		logg.Fatal("unknown method: %s", method)
	}
}

func executeAppendToBlob(ctx context.Context, sd keppel.StorageDriver, account models.ReducedAccount, args []string) {
	if len(args) != 3 {
		logg.Fatal("append-to-blob requires: <storage-id> <chunk-number> <chunk-data>")
	}

	storageID := args[0]
	chunkNumberStr := args[1]
	chunkData := args[2]

	chunkNumber, err := strconv.ParseUint(chunkNumberStr, 10, 32)
	if err != nil {
		logg.Fatal("invalid chunk number: %s", err.Error())
	}

	chunkDataBytes := []byte(chunkData)
	sizeBytes := uint64(len(chunkDataBytes))

	err = sd.AppendToBlob(ctx, account, storageID, uint32(chunkNumber), Some(sizeBytes), bytes.NewReader(chunkDataBytes))
	if err != nil {
		logg.Fatal("AppendToBlob failed: %s", err.Error())
	}

	logg.Info("chunk appended successfully")
}

func executeFinalizeBlob(ctx context.Context, sd keppel.StorageDriver, account models.ReducedAccount, args []string) {
	if len(args) != 2 {
		logg.Fatal("finalize-blob requires: <storage-id> <chunk-count>")
	}

	storageID := args[0]
	chunkCountStr := args[1]

	chunkCount, err := strconv.ParseUint(chunkCountStr, 10, 32)
	if err != nil {
		logg.Fatal("invalid chunk count: %s", err.Error())
	}

	err = sd.FinalizeBlob(ctx, account, storageID, uint32(chunkCount))
	if err != nil {
		logg.Fatal("FinalizeBlob failed: %s", err.Error())
	}

	logg.Info("blob finalized successfully")
}

func executeAbortBlobUpload(ctx context.Context, sd keppel.StorageDriver, account models.ReducedAccount, args []string) {
	if len(args) != 2 {
		logg.Fatal("abort-blob-upload requires: <storage-id> <chunk-count>")
	}

	storageID := args[0]
	chunkCountStr := args[1]

	chunkCount, err := strconv.ParseUint(chunkCountStr, 10, 32)
	if err != nil {
		logg.Fatal("invalid chunk count: %s", err.Error())
	}

	err = sd.AbortBlobUpload(ctx, account, storageID, uint32(chunkCount))
	if err != nil {
		logg.Fatal("AbortBlobUpload failed: %s", err.Error())
	}

	logg.Info("blob upload aborted successfully")
}

func executeReadBlob(ctx context.Context, sd keppel.StorageDriver, account models.ReducedAccount, args []string) {
	if len(args) != 1 {
		logg.Fatal("read-blob requires: <storage-id>")
	}

	storageID := args[0]

	contents, sizeBytes, err := sd.ReadBlob(ctx, account, storageID)
	if err != nil {
		logg.Fatal("ReadBlob failed: %s", err.Error())
	}
	defer contents.Close()

	contentBytes, err := io.ReadAll(contents)
	if err != nil {
		logg.Fatal("failed to read blob contents: %s", err.Error())
	}

	result := map[string]any{
		"contents":   string(contentBytes),
		"size_bytes": sizeBytes,
	}
	outputJSON(result)
}

func executeURLForBlob(ctx context.Context, sd keppel.StorageDriver, account models.ReducedAccount, args []string) {
	if len(args) != 1 {
		logg.Fatal("url-for-blob requires: <storage-id>")
	}

	storageID := args[0]

	url, err := sd.URLForBlob(ctx, account, storageID)
	if err != nil {
		logg.Fatal("URLForBlob failed: %s", err.Error())
	}

	logg.Info(url)
}

func executeDeleteBlob(ctx context.Context, sd keppel.StorageDriver, account models.ReducedAccount, args []string) {
	if len(args) != 1 {
		logg.Fatal("delete-blob requires: <storage-id>")
	}

	storageID := args[0]

	err := sd.DeleteBlob(ctx, account, storageID)
	if err != nil {
		logg.Fatal("DeleteBlob failed: %s", err.Error())
	}

	logg.Info("blob deleted successfully")
}

func executeReadManifest(ctx context.Context, sd keppel.StorageDriver, account models.ReducedAccount, args []string) {
	if len(args) != 2 {
		logg.Fatal("read-manifest requires: <repo-name> <digest>")
	}

	repoName := args[0]
	digestStr := args[1]

	d, err := digest.Parse(digestStr)
	if err != nil {
		logg.Fatal("invalid digest: %s", err.Error())
	}

	result, err := sd.ReadManifest(ctx, account, repoName, d)
	if err != nil {
		logg.Fatal("ReadManifest failed: %s", err.Error())
	}

	logg.Info(string(result))
}

func executeWriteManifest(ctx context.Context, sd keppel.StorageDriver, account models.ReducedAccount, args []string) {
	if len(args) != 3 {
		logg.Fatal("write-manifest requires: <repo-name> <digest> <content>")
	}

	repoName := args[0]
	digestStr := args[1]
	content := args[2]

	d, err := digest.Parse(digestStr)
	if err != nil {
		logg.Fatal("invalid digest: %s", err.Error())
	}
	err = sd.WriteManifest(ctx, account, repoName, d, []byte(content))
	if err != nil {
		logg.Fatal("WriteManifest failed: %s", err.Error())
	}

	logg.Info("manifest written successfully")
}

func executeDeleteManifest(ctx context.Context, sd keppel.StorageDriver, account models.ReducedAccount, args []string) {
	if len(args) != 2 {
		logg.Fatal("delete-manifest requires: <repo-name> <digest>")
	}

	repoName := args[0]
	digestStr := args[1]

	d, err := digest.Parse(digestStr)
	if err != nil {
		logg.Fatal("invalid digest: %s", err.Error())
	}

	err = sd.DeleteManifest(ctx, account, repoName, d)
	if err != nil {
		logg.Fatal("DeleteManifest failed: %s", err.Error())
	}

	logg.Info("manifest deleted successfully")
}

func executeReadTrivyReport(ctx context.Context, sd keppel.StorageDriver, account models.ReducedAccount, args []string) {
	if len(args) != 3 {
		logg.Fatal("read-trivy-report requires: <repo-name> <digest> <format>")
	}

	repoName := args[0]
	digestStr := args[1]
	format := args[2]

	d, err := digest.Parse(digestStr)
	if err != nil {
		logg.Fatal("invalid digest: %s", err.Error())
	}

	result, err := sd.ReadTrivyReport(ctx, account, repoName, d, format)
	if err != nil {
		logg.Fatal("ReadTrivyReport failed: %s", err.Error())
	}

	logg.Info(string(result))
}

func executeWriteTrivyReport(ctx context.Context, sd keppel.StorageDriver, account models.ReducedAccount, args []string) {
	if len(args) != 4 {
		logg.Fatal("write-trivy-report requires: <repo-name> <digest> <payload-content> <payload-format>")
	}

	repoName := args[0]
	digestStr := args[1]
	content := args[2]
	format := args[3]

	d, err := digest.Parse(digestStr)
	if err != nil {
		logg.Fatal("invalid digest: %s", err.Error())
	}

	err = sd.WriteTrivyReport(ctx, account, repoName, d, trivy.ReportPayload{
		Contents: io.NopCloser(strings.NewReader(content)),
		Format:   format,
	})
	if err != nil {
		logg.Fatal("WriteTrivyReport failed: %s", err.Error())
	}

	logg.Info("trivy report written successfully")
}

func executeDeleteTrivyReport(ctx context.Context, sd keppel.StorageDriver, account models.ReducedAccount, args []string) {
	if len(args) != 3 {
		logg.Fatal("delete-trivy-report requires: <repo-name> <digest> <format>")
	}

	repoName := args[0]
	digestStr := args[1]
	format := args[2]

	d, err := digest.Parse(digestStr)
	if err != nil {
		logg.Fatal("invalid digest: %s", err.Error())
	}

	err = sd.DeleteTrivyReport(ctx, account, repoName, d, format)
	if err != nil {
		logg.Fatal("DeleteTrivyReport failed: %s", err.Error())
	}

	logg.Info("trivy report deleted successfully")
}

func executeListStorageContents(ctx context.Context, sd keppel.StorageDriver, account models.ReducedAccount) {
	blobs, manifests, trivyReports, err := sd.ListStorageContents(ctx, account)
	if err != nil {
		logg.Fatal("ListStorageContents failed: %s", err.Error())
	}

	result := map[string]any{
		"blobs":         blobs,
		"manifests":     manifests,
		"trivy_reports": trivyReports,
	}
	outputJSON(result)
}

func executeCanSetupAccount(ctx context.Context, sd keppel.StorageDriver, account models.ReducedAccount) {
	err := sd.CanSetupAccount(ctx, account)
	if err != nil {
		logg.Fatal("CanSetupAccount failed: %s", err.Error())
	}

	logg.Info("account can be set up successfully")
}

func executeCleanupAccount(ctx context.Context, sd keppel.StorageDriver, account models.ReducedAccount) {
	err := sd.CleanupAccount(ctx, account)
	if err != nil {
		logg.Fatal("CleanupAccount failed: %s", err.Error())
	}

	logg.Info("account cleanup completed successfully")
}

func outputJSON(result any) {
	jsonBytes, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		logg.Fatal("failed to encode JSON result: %s", err.Error())
	}
	fmt.Println(string(jsonBytes))
}
