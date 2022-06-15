/******************************************************************************
*
*  Copyright 2022 SAP SE
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

//Package bininfo contains information about the current binary and process.
//Most of the information available through this interface is filled at build
//time using the -X linker flag.
//
//This package can be considered an interface between the application (which
//provides the requisite data at build time and runtime) and various places
//around our internal libraries (which use this data, e.g. to construct
//User-Agent headers or log messages).
//
//When using <https://github.com/sapcc/go-makefile-maker>, go-makefile-maker
//will detect when go-api-declarations is listed as a dependency in the
//application's go.mod file and generate the appropriate linker flags
//automatically.
package bininfo

import "fmt"

var (
	//These variables are filled at buildtime with the -X linker flag. Everything
	//except for `binName` may be empty if the build could not determine a value.
	binName   string
	version   string
	commit    string
	buildDate string
	//This always starts blank and is filled by SetTaskName().
	taskName string
)

//Component returns the name of the current binary, followed by the name of the
//current process's task if one has provided via SetTaskName(). This string can
//be used to identify the current process, e.g. in User-Agent headers.
//
//For example, the command `tenso worker` calls `SetTaskName("worker")`, so
//Component() will return "tenso-worker".
func Component() string {
	if taskName == "" {
		return binName
	}
	return fmt.Sprintf("%s-%s", binName, taskName)
}

//SetTaskName identifies the subcommand selected for the current process. This
//setting influences the output of Component().
func SetTaskName(name string) {
	taskName = name
}

//Version returns the version string provided at build time, or "" if none was
//provided.
func Version() string {
	return version
}

//VersionOr returns the version string provided at build time, or the fallback
//value if none was provided. A common invocation is `VersionOr("unknown")`.
func VersionOr(fallback string) string {
	if version == "" {
		return fallback
	}
	return version
}

//Commit returns the commit string provided at build time, or "" if none was
//provided.
func Commit() string {
	return commit
}

//CommitOr returns the commit string provided at build time, or the fallback
//value if none was provided. A common invocation is `CommitOr("unknown")`.
func CommitOr(fallback string) string {
	if commit == "" {
		return fallback
	}
	return commit
}

//BuildDate returns the buildDate string provided at build time, or "" if none was
//provided.
func BuildDate() string {
	return buildDate
}

//BuildDateOr returns the build date string, or the fallback value if none is
//known. A common invocation is `BuildDateOr("unknown")`.
func BuildDateOr(fallback string) string {
	if buildDate == "" {
		return fallback
	}
	return buildDate
}
