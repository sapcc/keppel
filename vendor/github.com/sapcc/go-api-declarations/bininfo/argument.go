// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package bininfo

import (
	"fmt"
	"os"
)

// HandleVersionArgument prints the version string and exits if the first argument to the program is --version
// This function is recommended for simple go programs without an argument parsing library and should be called very early in the main function.
func HandleVersionArgument() {
	args := os.Args[1:]
	if len(args) > 0 {
		if args[0] == "--version" {
			fmt.Printf("%s version %s\n", binName, version)
			os.Exit(0)
		}
	}
}
