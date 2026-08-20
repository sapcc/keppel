// SPDX-FileCopyrightText: 2026 Stefan Majewsky <majewsky@gmx.net>
// SPDX-License-Identifier: Apache-2.0

package testcapture

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
)

// Result is returned by func [Capture].
type Result struct {
	// Outcome describes how the test ended.
	Outcome Outcome
	// Panic contains a payload recovered from a panic(), if Outcome is [OutcomePanicked].
	Panic any
	// Messages contains log lines captured from t.Log() calls, or functions calling t.Log(), such as t.Error() and t.Fatal();
	// as well as data captured from t.Output().Write() calls.
	Messages []Message
	// Attrs contains attributes captured in t.Attr() calls.
	Attrs map[string]string
	// Artifacts holds the contents of any regular files that were created below t.ArtifactDir(), keyed with the path relative to t.ArtifactDir().
	Artifacts map[string]string
}

// Outcome is an enum.
// It appears in type [Result].
type Outcome string

const (
	// OutcomeFinished describes a [Capture] that ended with the test running to completion.
	OutcomeFinished Outcome = "finished"
	// OutcomeFailed describes a [Capture] that ended early because of a t.FailNow() call.
	OutcomeFailed Outcome = "failed"
	// OutcomeSkipped describes a [Capture] that ended early because of a t.SkipNow() call.
	OutcomeSkipped Outcome = "skipped"
	// OutcomePanicked describes a [Capture] that ended early because of a panic() call.
	OutcomePanicked Outcome = "panicked"
)

// Message is a piece of log output captured by func [Capture].
// It appears in type [Result].
//   - Each call to t.Log(), t.Logf() or their derived functions results in one Message instance of type [Log].
//   - Writing into t.Output() between two calls to t.Log(), t.Logf() etc. results in a single Message instance of type [Output], even if Write() is called multiple times.
type Message struct {
	Message string
	Type    MessageType
}

// Log is a shorthand for constructing [Message] objects of type [MessageTypeLog].
func Log[T interface{ ~string }](message T) Message {
	return Message{string(message), MessageTypeLog}
}

// Output is a shorthand for constructing [Message] objects of type [MessageTypeOutput].
func Output[T interface{ ~string | ~[]byte }](message T) Message {
	return Message{string(message), MessageTypeOutput}
}

// MessageType is an enum.
// It appears in type [Message].
type MessageType string

const (
	// MessageTypeLog describes [Message] instances created by calls to t.Log(), t.Logf(), or functions calling them, such as t.Error() and t.Fatal().
	MessageTypeLog MessageType = "log"
	// MessageTypeOutput describes [Message] instances created by calls to t.Output().Write().
	MessageTypeOutput MessageType = "output"
)

// Capture executes a test function with a stub implementation of [TestingTB] that captures all calls to it.
// It is intended for unit-testing test assertions.
//
// The name argument is what will be reported in t.Name() within the test.
func Capture(ctx context.Context, name string, test func(TestingTB)) Result {
	r := Result{
		Outcome: OutcomeFinished, // can be overridden by Fail() or SkipNow()
	}
	executeCapture(ctx, name, &r, test)
	return r
}

// capturer is the implementation of [TestingTB] used by func [Capture].
type capturer struct {
	context  context.Context
	cleanups []func()
	name     string
	result   *Result
	state    struct {
		ArtifactDir string
	}

	cleanupsMutex sync.Mutex   // lock for access to the `cleanups` field
	resultMutex   sync.RWMutex // lock for access to the `result` field
	stateMutex    sync.Mutex   // lock for access to the `state` field
	nonlocalMutex sync.Mutex   // lock for non-local effects like Setenv() or filesystem operations
}

func executeCapture(ctx context.Context, name string, r *Result, test func(TestingTB)) {
	ctx, cancel := context.WithCancel(ctx)
	t := capturer{
		context:  ctx,
		cleanups: nil,
		name:     name,
		result:   r,
	}
	defer func() {
		t.setOutcome(recover())
		cancel() // T.Context() demands that the context be canceled before any cleanup handlers
		for _, cleanup := range slices.Backward(t.cleanups) {
			cleanup()
		}
	}()
	test(&t)
}

func (t *capturer) setOutcome(panicPayload any) {
	t.resultMutex.Lock()
	defer t.resultMutex.Unlock()
	if panicPayload == nil {
		return
	} else if outcome, ok := panicPayload.(Outcome); ok {
		t.result.Outcome = outcome
	} else {
		t.result.Outcome = OutcomePanicked
		t.result.Panic = panicPayload
	}
}

func (t *capturer) pushOutput(buf []byte, msgType MessageType) {
	t.resultMutex.Lock()
	defer t.resultMutex.Unlock()

	// try to merge consecutive t.Output().Write() calls together
	if msgType == MessageTypeOutput && len(t.result.Messages) > 0 {
		idx := len(t.result.Messages) - 1
		msg := t.result.Messages[idx]
		if msg.Type == MessageTypeOutput {
			msg.Message = msg.Message + string(buf)
			t.result.Messages[idx] = msg
			return
		}
	}

	t.result.Messages = append(t.result.Messages, Message{
		Message: string(buf),
		Type:    msgType,
	})
}

var tempdirID atomic.Uint64

func pickTempdir() (string, error) {
	path := filepath.Join(os.TempDir(), fmt.Sprintf("gg-assert-capture-%d", tempdirID.Add(1)))
	return path, os.MkdirAll(path, 0777)
}

func collectArtifacts(dirPath string) (map[string]string, error) {
	dir, err := os.OpenRoot(dirPath)
	if err != nil {
		return nil, err
	}
	dirFS := dir.FS()

	result := make(map[string]string)
	err = fs.WalkDir(dirFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			buf, err := fs.ReadFile(dirFS, path)
			if err != nil {
				return err
			}
			result[path] = string(buf)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return result, os.RemoveAll(dirPath)
}

// ArtifactDir implements the [TestingTB] interface.
func (t *capturer) ArtifactDir() string {
	t.stateMutex.Lock()
	defer t.stateMutex.Unlock()
	if t.state.ArtifactDir == "" {
		path, err := pickTempdir()
		if err != nil {
			t.Fatal("in t.ArtifactDir(): ", err)
		}
		t.state.ArtifactDir = path

		t.Cleanup(func() {
			artifacts, err := collectArtifacts(path)
			if err == nil {
				t.resultMutex.Lock()
				defer t.resultMutex.Unlock()
				t.result.Artifacts = artifacts
			} else {
				t.Error(err)
			}
		})
	}
	return t.state.ArtifactDir
}

// Attr implements the [TestingTB] interface.
func (t *capturer) Attr(key, value string) {
	t.resultMutex.Lock()
	defer t.resultMutex.Unlock()
	if t.result.Attrs == nil {
		t.result.Attrs = make(map[string]string)
	}
	t.result.Attrs[key] = value
}

// Chdir implements the [TestingTB] interface.
func (t *capturer) Chdir(dir string) {
	t.doChdir(dir)

	// the following is done outside of doChdir() because t.Setenv() also locks t.nonlocalMutex
	switch runtime.GOOS {
	case "windows", "plan9":
		// these platforms do not use the PWD variable
	default:
		dir, err := os.Getwd() // returns an absolute path even if `dir` is not one
		if err != nil {
			t.Fatal(err)
		}
		t.Setenv("PWD", dir)
	}
}

func (t *capturer) doChdir(dir string) {
	t.nonlocalMutex.Lock()
	defer t.nonlocalMutex.Unlock()

	oldDir, err := os.Open(".")
	if err != nil {
		t.Fatal(err)
	}
	err = os.Chdir(dir)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		err := oldDir.Chdir()
		if err != nil {
			t.Error("could not reset cwd changed by t.Chdir(): ", err)
		}
	})
}

// Cleanup implements the [TestingTB] interface.
func (t *capturer) Cleanup(action func()) {
	t.cleanupsMutex.Lock()
	defer t.cleanupsMutex.Unlock()
	t.cleanups = append(t.cleanups, action)
}

// Context implements the [TestingTB] interface.
func (t *capturer) Context() context.Context {
	return t.context
}

// Error implements the [TestingTB] interface.
func (t *capturer) Error(args ...any) {
	t.Log(args...)
	t.Fail()
}

// Errorf implements the [TestingTB] interface.
func (t *capturer) Errorf(format string, args ...any) {
	t.Logf(format, args...)
	t.Fail()
}

// Fail implements the [TestingTB] interface.
func (t *capturer) Fail() {
	t.resultMutex.Lock()
	defer t.resultMutex.Unlock()
	t.result.Outcome = OutcomeFailed
}

// Failed implements the [TestingTB] interface.
func (t *capturer) Failed() bool {
	t.resultMutex.RLock()
	defer t.resultMutex.RUnlock()
	return t.result.Outcome == OutcomeFailed
}

// FailNow implements the [TestingTB] interface.
func (t *capturer) FailNow() {
	panic(OutcomeFailed)
}

// Fatal implements the [TestingTB] interface.
func (t *capturer) Fatal(args ...any) {
	t.Log(args...)
	t.FailNow()
}

// Fatalf implements the [TestingTB] interface.
func (t *capturer) Fatalf(format string, args ...any) {
	t.Logf(format, args...)
	t.FailNow()
}

// Helper implements the [TestingTB] interface.
func (t *capturer) Helper() {
	// no-op because we do not collect file and line information at the moment
}

// Log implements the [TestingTB] interface.
func (t *capturer) Log(args ...any) {
	t.pushOutput(fmt.Append(nil, args...), MessageTypeLog)
}

// Logf implements the [TestingTB] interface.
func (t *capturer) Logf(format string, args ...any) {
	t.pushOutput(fmt.Appendf(nil, format, args...), MessageTypeLog)
}

// Name implements the [TestingTB] interface.
func (t *capturer) Name() string {
	return t.name
}

// Output implements the [TestingTB] interface.
func (t *capturer) Output() io.Writer {
	return outputCapturer{t}
}

type outputCapturer struct {
	t *capturer
}

// Write implements the [io.Writer] interface.
func (c outputCapturer) Write(buf []byte) (int, error) {
	c.t.pushOutput(buf, MessageTypeOutput)
	return len(buf), nil
}

// Setenv implements the [TestingTB] interface.
func (t *capturer) Setenv(key, value string) {
	t.nonlocalMutex.Lock()
	defer t.nonlocalMutex.Unlock()

	oldValue, hasOldValue := os.LookupEnv(key)
	os.Setenv(key, value)

	t.Cleanup(func() {
		if hasOldValue {
			os.Setenv(key, oldValue)
		} else {
			os.Unsetenv(key)
		}
	})
}

// Skip implements the [TestingTB] interface.
func (t *capturer) Skip(args ...any) {
	t.Log(args...)
	t.SkipNow()
}

// Skipf implements the [TestingTB] interface.
func (t *capturer) Skipf(format string, args ...any) {
	t.Logf(format, args...)
	t.SkipNow()
}

// SkipNow implements the [TestingTB] interface.
func (t *capturer) SkipNow() {
	panic(OutcomeSkipped)
}

// Skipped implements the [TestingTB] interface.
func (t *capturer) Skipped() bool {
	t.resultMutex.RLock()
	defer t.resultMutex.RUnlock()
	return t.result.Outcome == OutcomeSkipped
}

// TempDir implements the [TestingTB] interface.
func (t *capturer) TempDir() string {
	path, err := pickTempdir()
	if err != nil {
		t.Fatal("in t.TempDir(): ", err)
	}
	t.Cleanup(func() {
		err := os.RemoveAll(path)
		if err != nil {
			t.Error(err)
		}
	})
	return path
}
