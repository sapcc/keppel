// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package testdiff

import (
	"os"
	"os/exec"
)

// DiffAgainstFixtureFile checks that the contents of the file at the given
// path are equal to the provided bytestring. If not, the provided bytestring
// will be stored at `path + ".actual"` to allow the user to inspect the diff.
func DiffAgainstFixtureFile(path string, actual []byte) error {
	// write actual content to file to make it easy to copy the computed result over
	// to the fixture path when a new test is added or an existing one is modified
	actualPath := path + ".actual"
	err := os.WriteFile(actualPath, actual, 0o666)
	if err != nil {
		return err
	}
	defer func() {
		// if there is no diff, we do not need to retain the ".actual" file;
		// this is especially important because, if `reuse lint` runs later as part
		// of the full test suite, it might be confused about the licensing of this
		// irrelevant temporary file
		if err == nil {
			err = os.Remove(actualPath)
		}
	}()

	cmd := exec.Command("diff", "-u", path, actualPath)
	cmd.Stdin = nil
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
