/******************************************************************************
*
*  Copyright 2020 SAP SE
*
*  Licensed under the Apache License, Version 2.0 (the "License");
*  you may not use this file except in compliance with the License.
*  You may obtain a copy of the License at
*
*      http://www.apache.org/licenses/LICENSE-2.0
*
*  Unless required by applicable law or agreed to in writing, software
*  distributed under the License is distributed on an "AS IS" BASIS,
*  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
*  See the License for the specific language governing permissions and
*  limitations under the License.
*
******************************************************************************/

package validatecmd

import (
	"os"
	"strings"

	"github.com/opencontainers/go-digest"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/keppel/internal/client"
	"github.com/spf13/cobra"
)

var (
	authUserName string
	authPassword string
)

//AddCommandTo mounts this command into the command hierarchy.
func AddCommandTo(parent *cobra.Command) {
	cmd := &cobra.Command{
		Use:     "validate <image>",
		Example: "  keppel validate registry.example.org/library/alpine:3.9",
		Short:   "Pulls an image and validates that its contents are intact.",
		Long: `Pulls an image and validates that its contents are intact.
If the image is in a Keppel replica account, this ensures that the image is replicated as a side effect.`,
		Args: cobra.ExactArgs(1),
		Run:  run,
	}
	cmd.PersistentFlags().StringVar(&authUserName, "username", "", "User name (only required for non-public images).")
	cmd.PersistentFlags().StringVar(&authPassword, "password", "", "Password (only required for non-public images).")
	parent.AddCommand(cmd)
}

type logger struct{}

//LogManifest implements the client.ValidationLogger interface.
func (l logger) LogManifest(reference string, level int, err error) {
	indent := strings.Repeat("  ", level)
	if err == nil {
		logg.Info("%smanifest %s looks good", indent, reference)
	} else {
		logg.Error("%smanifest %s validation failed: %s", indent, reference, err.Error())
	}
}

//LogBlob implements the client.ValidationLogger interface.
func (l logger) LogBlob(d digest.Digest, level int, err error) {
	indent := strings.Repeat("  ", level)
	if err == nil {
		logg.Info("%sblob     %s looks good", indent, d.String())
	} else {
		logg.Error("%sblob     %s validation failed: %s", indent, d.String(), err.Error())
	}
}

func run(cmd *cobra.Command, args []string) {
	ref, interpretation, err := client.ParseImageReference(args[0])
	logg.Info("interpreting %s as %s", args[0], interpretation)
	if err != nil {
		logg.Fatal(err.Error())
	}

	c := &client.RepoClient{
		Host:     ref.Host,
		RepoName: ref.RepoName,
		UserName: authUserName,
		Password: authPassword,
	}
	err = c.ValidateManifest(ref.Reference, logger{})
	if err != nil {
		os.Exit(1)
	}
}
