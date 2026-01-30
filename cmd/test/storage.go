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
	"github.com/sapcc/go-bits/must"
	"github.com/spf13/cobra"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
	"github.com/sapcc/keppel/internal/trivy"
)

var (
	accountName             string
	accountAuthTenantID     string
	authDriverType          string
	storageDriverType       string
	storageDriverParamsJSON string
)

// AddStorageCommandTo mounts the storage test command into the test-driver command hierarchy.
func AddStorageCommandTo(parent *cobra.Command) {
	storageCmd := &cobra.Command{
		Use:     "storage <driver-impl> <method> <args...>",
		Example: "  keppel test-driver storage --ad keystone --sd swift -a myaccount -t 3d9880d658e34770 read-manifest repo sha256:abc123",
		Short:   "Manual test harness for storage driver implementations.",
		Long:    `Manual test harness for storage driver implementations. Performs the minimum required setup to obtain the respective storage driver instance, executes the method and then displays the result.`,
	}

	storageCmd.PersistentFlags().StringVarP(&accountName, "account-name", "a", "", "Account name (required)")
	must.Succeed(storageCmd.MarkPersistentFlagRequired("account-name"))
	storageCmd.PersistentFlags().StringVarP(&accountAuthTenantID, "account-auth-tenant-id", "t", "", "Account auth tenant ID (required)")
	must.Succeed(storageCmd.MarkPersistentFlagRequired("account-auth-tenant-id"))
	storageCmd.PersistentFlags().StringVarP(&authDriverType, "ad", "", "trivial", "Type name for auth driver")
	must.Succeed(storageCmd.MarkPersistentFlagRequired("ad"))
	storageCmd.PersistentFlags().StringVarP(&storageDriverType, "sd", "", "", "Type name for storage driver")
	must.Succeed(storageCmd.MarkPersistentFlagRequired("sd"))
	storageCmd.PersistentFlags().StringVarP(&storageDriverParamsJSON, "params", "p", `{}`, "Parameters for storage driver (encoded as JSON)")

	storageCmd.AddCommand(
		&cobra.Command{
			Use:  "append-to-blob <storage-id> <chunk-number> <data>",
			Args: cobra.ExactArgs(3),
			Run:  wrapStorageCommand(executeAppendToBlob),
		},
		&cobra.Command{
			Use:  "finalize-blob <storage-id> <chunk-count>",
			Args: cobra.ExactArgs(2),
			Run:  wrapStorageCommand(executeFinalizeBlob),
		},
		&cobra.Command{
			Use:  "abort-blob-upload <storage-id> <chunk-count>",
			Args: cobra.ExactArgs(2),
			Run:  wrapStorageCommand(executeAbortBlobUpload),
		},
		&cobra.Command{
			Use:  "read-blob <storage-id>",
			Args: cobra.ExactArgs(1),
			Run:  wrapStorageCommand(executeReadBlob),
		},
		&cobra.Command{
			Use:  "url-for-blob <storage-id>",
			Args: cobra.ExactArgs(1),
			Run:  wrapStorageCommand(executeURLForBlob),
		},
		&cobra.Command{
			Use:  "delete-blob <storage-id>",
			Args: cobra.ExactArgs(1),
			Run:  wrapStorageCommand(executeDeleteBlob),
		},
		&cobra.Command{
			Use:  "read-manifest <repo-name> <digest>",
			Args: cobra.ExactArgs(2),
			Run:  wrapStorageCommand(executeReadManifest),
		},
		&cobra.Command{
			Use:  "write-manifest <repo-name> <digest> <content>",
			Args: cobra.ExactArgs(3),
			Run:  wrapStorageCommand(executeWriteManifest),
		},
		&cobra.Command{
			Use:  "delete-manifest <repo-name> <digest>",
			Args: cobra.ExactArgs(2),
			Run:  wrapStorageCommand(executeDeleteManifest),
		},
		&cobra.Command{
			Use:  "read-trivy-report <repo-name> <digest> <format>",
			Args: cobra.ExactArgs(3),
			Run:  wrapStorageCommand(executeReadTrivyReport),
		},
		&cobra.Command{
			Use:  "write-trivy-report <repo-name> <digest> <payload-content> <payload-format>",
			Args: cobra.ExactArgs(4),
			Run:  wrapStorageCommand(executeWriteTrivyReport),
		},
		&cobra.Command{
			Use:  "delete-trivy-report <repo-name> <digest> <format>",
			Args: cobra.ExactArgs(3),
			Run:  wrapStorageCommand(executeDeleteTrivyReport),
		},
		&cobra.Command{
			Use:  "list-storage-contents",
			Args: cobra.NoArgs,
			Run:  wrapStorageCommand(executeListStorageContents),
		},
		&cobra.Command{
			Use:  "can-setup-account",
			Args: cobra.NoArgs,
			Run:  wrapStorageCommand(executeCanSetupAccount),
		},
		&cobra.Command{
			Use:  "cleanup-account",
			Args: cobra.NoArgs,
			Run:  wrapStorageCommand(executeCleanupAccount),
		},
	)

	parent.AddCommand(storageCmd)
}

// wrapStorageCommand takes a handler that tests a StorageDriver method,
// and wraps it so that it can be put as a cobra.Command.Run function.
//
// The resulting closure takes care of parsing arguments provided in cmdline flags
// and preparing all the arguments that are needed to call the test function.
func wrapStorageCommand(action func(context.Context, keppel.StorageDriver, models.ReducedAccount, []string)) func(*cobra.Command, []string) {
	return func(cmd *cobra.Command, args []string) {
		ctx := cmd.Context()
		cfg := keppel.Configuration{}

		account := models.ReducedAccount{
			Name:         models.AccountName(accountName),
			AuthTenantID: accountAuthTenantID,
		}

		var authConfig string
		switch authDriverType {
		case "trivial":
			authConfig = `{"type":"trivial","params":{"username":"test","password":"test"}}`
		case "keystone":
			authConfig = `{"type":"keystone","params":{"oslo_policy_path":"cmd/test/policy.json"}}`
		}
		ad := must.Return(keppel.NewAuthDriver(ctx, authConfig, nil))

		if !json.Valid([]byte(storageDriverParamsJSON)) {
			logg.Fatal("value provided to --params is not valid JSON")
		}
		storageConfig := fmt.Sprintf(`{"type":%s,"params":%s}`,
			must.Return(json.Marshal(storageDriverType)),
			storageDriverParamsJSON,
		)
		sd := must.Return(keppel.NewStorageDriver(storageConfig, ad, cfg))

		action(ctx, sd, account, args)
	}
}

func executeAppendToBlob(ctx context.Context, sd keppel.StorageDriver, account models.ReducedAccount, args []string) {
	storageID := args[0]
	chunkNumberStr := args[1]
	chunkData := args[2]

	chunkNumber := uint32(must.Return(strconv.ParseUint(chunkNumberStr, 10, 32))) //nolint:gosec // no overflow possible

	chunkDataBytes := []byte(chunkData)
	sizeBytes := uint64(len(chunkDataBytes))

	err := sd.AppendToBlob(ctx, account, storageID, chunkNumber, Some(sizeBytes), bytes.NewReader(chunkDataBytes))
	if err != nil {
		logg.Fatal("AppendToBlob failed: %s", err.Error())
	}

	logg.Info("chunk appended successfully")
}

func executeFinalizeBlob(ctx context.Context, sd keppel.StorageDriver, account models.ReducedAccount, args []string) {
	storageID := args[0]
	chunkCountStr := args[1]

	chunkCount := uint32(must.Return(strconv.ParseUint(chunkCountStr, 10, 32))) //nolint:gosec // no overflow possible

	err := sd.FinalizeBlob(ctx, account, storageID, chunkCount)
	if err != nil {
		logg.Fatal("FinalizeBlob failed: %s", err.Error())
	}

	logg.Info("blob finalized successfully")
}

func executeAbortBlobUpload(ctx context.Context, sd keppel.StorageDriver, account models.ReducedAccount, args []string) {
	storageID := args[0]
	chunkCountStr := args[1]

	chunkCount := uint32(must.Return(strconv.ParseUint(chunkCountStr, 10, 32))) //nolint:gosec // no overflow possible

	err := sd.AbortBlobUpload(ctx, account, storageID, chunkCount)
	if err != nil {
		logg.Fatal("AbortBlobUpload failed: %s", err.Error())
	}

	logg.Info("blob upload aborted successfully")
}

func executeReadBlob(ctx context.Context, sd keppel.StorageDriver, account models.ReducedAccount, args []string) {
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
	storageID := args[0]

	url, err := sd.URLForBlob(ctx, account, storageID)
	if err != nil {
		logg.Fatal("URLForBlob failed: %s", err.Error())
	}

	logg.Info(url)
}

func executeDeleteBlob(ctx context.Context, sd keppel.StorageDriver, account models.ReducedAccount, args []string) {
	storageID := args[0]

	err := sd.DeleteBlob(ctx, account, storageID)
	if err != nil {
		logg.Fatal("DeleteBlob failed: %s", err.Error())
	}

	logg.Info("blob deleted successfully")
}

func executeReadManifest(ctx context.Context, sd keppel.StorageDriver, account models.ReducedAccount, args []string) {
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

func executeListStorageContents(ctx context.Context, sd keppel.StorageDriver, account models.ReducedAccount, _ []string) {
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

func executeCanSetupAccount(ctx context.Context, sd keppel.StorageDriver, account models.ReducedAccount, _ []string) {
	err := sd.CanSetupAccount(ctx, account)
	if err != nil {
		logg.Fatal("CanSetupAccount failed: %s", err.Error())
	}

	logg.Info("account can be set up successfully")
}

func executeCleanupAccount(ctx context.Context, sd keppel.StorageDriver, account models.ReducedAccount, _ []string) {
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
