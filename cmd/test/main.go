// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package testcmd

import (
	"github.com/spf13/cobra"
)

// AddCommandTo mounts this command into the command hierarchy.
func AddCommandTo(parent *cobra.Command) {
	testDriverCmd := &cobra.Command{
		Use:     "test-driver <driver-type> <driver-impl> <method> <args...>",
		Example: "  keppel test-driver storage swift read-manifest repo sha256:abc123",
		Short:   "Manual test harness for driver implementations.",
		Long:    `Manual test harness for driver implementations. Performs the minimum required setup to obtain the respective driver instance, executes the method and then displays the result.`,
	}

	AddStorageCommandTo(testDriverCmd)

	parent.AddCommand(testDriverCmd)
}
