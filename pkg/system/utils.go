package system

import (
	"bytes"
	"fmt"
	"io"
	"math/rand"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/filecoin-project/bacalhau/pkg/executor"
	"github.com/rs/zerolog/log"
)

// TODO: #282 we need these to avoid stream based deadlocks
// https://go-review.googlesource.com/c/go/+/42271/3/misc/android/go_android_exec.go#37

var Stdout = struct{ io.Writer }{os.Stdout}
var Stderr = struct{ io.Writer }{os.Stderr}

func RunCommand(command string, args []string) error {
	log.Trace().Msgf(`Command: %s %s`, command, args)
	cmd := exec.Command(command, args...)
	cmd.Stderr = Stderr
	cmd.Stdout = Stdout
	return cmd.Run()
}

// same as run command but also returns buffers for stdout and stdin
func RunTeeCommand(command string, args []string) (stdoutBuf, stderrBuf *bytes.Buffer, err error) {
	stdoutBuf = new(bytes.Buffer)
	stderrBuf = new(bytes.Buffer)

	log.Trace().Msgf("Command: %s %s", command, args)
	cmd := exec.Command(command, args...)

	cmd.Stdout = io.MultiWriter(Stdout, stdoutBuf)
	cmd.Stderr = io.MultiWriter(Stderr, stderrBuf)
	return stdoutBuf, stderrBuf, cmd.Run()
}

func TryUntilSucceedsN(f func() error, desc string, retries int) error {
	attempt := 0
	for {
		err := f()
		if err != nil {
			if attempt > retries {
				return err
			} else {
				log.Trace().Msgf("Error %s: %v, pausing and trying again...", desc, err)
				time.Sleep(1 * time.Second)
			}
		} else {
			return nil
		}
		attempt++
	}
}

func RunCommandGetResults(command string, args []string) (string, error) {
	log.Trace().Msgf("Command: %s %s", command, args)
	cmd := exec.Command(command, args...)
	result, err := cmd.CombinedOutput()
	return string(result), err
}

func RunCommandGetStdoutAndStderr(command string, args []string) (stdout, stderr string, err error) {
	stdoutBuf := new(bytes.Buffer)
	stderrBuf := new(bytes.Buffer)

	log.Trace().Msgf("Command: %s %s", command, args)
	cmd := exec.Command(command, args...)
	cmd.Stdout = stdoutBuf
	cmd.Stderr = stderrBuf

	err = cmd.Run()
	return stdoutBuf.String(), stderrBuf.String(), err
}

func RunCommandGetResultsEnv(command string, args, env []string) (string, error) {
	log.Trace().Msgf("Command: %s %s", command, args)
	cmd := exec.Command(command, args...)
	cmd.Env = env
	result, err := cmd.CombinedOutput()
	return string(result), err
}

// TODO: Pretty high priority to allow this to be configurable to a different directory than $HOME/.bacalhau
func GetSystemDirectory(path string) (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s/.bacalhau/%s", homeDir, path), nil
}

func EnsureSystemDirectory(path string) (string, error) {
	path, err := GetSystemDirectory(path)
	if err != nil {
		return "", err
	}

	log.Trace().Msgf("Enforcing creation of results dir: %s", path)

	err = RunCommand("mkdir", []string{
		"-p",
		path,
	})
	return path, err
}

func GetResultsDirectory(jobID, hostID string) string {
	return fmt.Sprintf("results/%s/%s", ShortID(jobID), hostID)
}

func ShortID(id string) string {
	parts := strings.Split(id, "-")
	return parts[0]
}

func StringArrayContains(s []string, e string) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}

func MapStringArray(vs []string, f func(string) string) []string {
	vsm := make([]string, len(vs))
	for i, v := range vs {
		vsm[i] = f(v)
	}
	return vsm
}

func MapByteArray(vs []byte, f func(byte) byte) []byte {
	vsm := make([]byte, len(vs))
	for i, v := range vs {
		vsm[i] = f(v)
	}
	return vsm
}

func GetJobStateStringArray(states []executor.JobStateType) []string {
	ret := []string{}
	for _, state := range states {
		ret = append(ret, state.String())
	}
	return ret
}

func ShortString(s string, n int) string {
	if len(s) < n {
		return s
	}
	return s[0:n] + "..."
}

const letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

func GetRandomString(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = letterBytes[rand.Intn(len(letterBytes))]
	}
	return string(b)
}

// PathExists returns whether the given file or directory exists
func PathExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}
