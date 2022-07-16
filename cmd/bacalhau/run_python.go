package bacalhau

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/filecoin-project/bacalhau/pkg/job"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var deterministic bool
var command string
var requirementsPath string
var contextPath string

//nolint:gochecknoinits
func init() {
	// determinism flag
	runPythonCmd.PersistentFlags().BoolVar(
		&deterministic, "deterministic", true,
		`Enforce determinism: run job in a single-threaded wasm runtime with `+
			`no sources of entropy. NB: this will make the python runtime execute`+
			`in an environment where only some librarie are supported, see `+
			`https://pyodide.org/en/stable/usage/packages-in-pyodide.html`,
	)
	runPythonCmd.PersistentFlags().StringSliceVarP(
		&jobInputVolumes, "input-volumes", "v", []string{},
		`cid:path of the input data volumes`,
	)
	runPythonCmd.PersistentFlags().StringSliceVarP(
		&jobOutputVolumes, "output-volumes", "o", []string{},
		`name:path of the output data volumes`,
	)
	runPythonCmd.PersistentFlags().StringSliceVarP(
		&jobEnv, "env", "e", []string{},
		`The environment variables to supply to the job (e.g. --env FOO=bar --env BAR=baz)`,
	)
	// TODO: concurrency should be factored out (at least up to run, maybe
	// shared with docker and wasm raw commands too)
	runPythonCmd.PersistentFlags().IntVar(
		&jobConcurrency, "concurrency", 1,
		`How many nodes should run the job`,
	)
	runPythonCmd.PersistentFlags().StringVarP(
		&command, "command", "c", "",
		`Program passed in as string (like python)`,
	)
	runPythonCmd.PersistentFlags().StringVarP(
		&requirementsPath, "requirement", "r", "",
		`Install from the given requirements file. (like pip)`, // TODO: This option can be used multiple times.
	)
	runPythonCmd.PersistentFlags().StringVar(
		// TODO: consider replacing this with context-glob, default to
		// "./**/*.py|./requirements.txt", OR .bacalhau_ignore
		&contextPath, "context-path", ".",
		"Path to context (e.g. python code) to send to server (via public IPFS network) "+
			"for execution (max 10MiB). Set to empty string to disable",
	)
	runPythonCmd.PersistentFlags().StringVar(
		&jobVerifier, "verifier", "ipfs",
		`What verification engine to use to run the job`,
	)
}

// TODO: move the adapter code (from wasm to docker) into a wasm executor, so
// that the compute node can verify the job knowing that it was run properly,
// rather than doing the translation in, and thereby trusting, the client (to
// set up the wasm environment to be determinstic)

var runPythonCmd = &cobra.Command{
	Use:   "python",
	Short: "Run a python job on the network",
	Args:  cobra.MinimumNArgs(0),
	RunE: func(cmd *cobra.Command, cmdArgs []string) error { // nolint

		// error if determinism is false
		if !deterministic {
			return fmt.Errorf("determinism=false not supported yet " +
				"(python only supports wasm backend with forced determinism)")
		}

		// engineType := executor.EngineLanguage

		// var engineType executor.EngineType
		// if deterministic {
		// 	engineType = executor.EngineWasm
		// } else {
		// 	engineType = executor.EngineDocker
		// }

		// verifierType := verifier.VerifierIpfs // this does nothing right now?

		// if engineType == executor.EngineWasm {

		// 	// pythonFile := cmdArgs[0]
		// 	// TODO: expose python file on ipfs
		// 	jobImage := ""
		// 	jobEntrypoint := []string{}

		// }
		// return nil

		// TODO: prepare context

		var programPath string
		if len(cmdArgs) > 0 {
			programPath = cmdArgs[0]
		}

		if command == "" && programPath == "" {
			return fmt.Errorf("must specify an inline command or a path to a python file")
		}

		// TODO: implement ConstructLanguageJob and switch to it
		spec, deal, err := job.ConstructLanguageJob(
			jobInputVolumes,
			jobOutputVolumes,
			[]string{}, // no env vars (yet)
			jobConcurrency,
			"python",
			"3.10",
			command,
			programPath,
			requirementsPath,
			"",
			deterministic,
		)
		if err != nil {
			return err
		}

		var buf bytes.Buffer

		if contextPath == "." && requirementsPath == "" && programPath == "" {
			log.Info().Msgf("no program or requirements specified, not uploading context - set --context-path to full path to force context upload")
			contextPath = ""
		}

		if contextPath != "" {
			// construct a tar file from the contextPath directory
			// tar + gzip
			log.Info().Msgf("uploading %s to server to execute command in context, press Ctrl+C to cancel", contextPath)
			time.Sleep(1 * time.Second)
			err = compress(contextPath, &buf)
			if err != nil {
				return err
			}

			// check size of buf
			if buf.Len() > 10*1024*1024 {
				return fmt.Errorf("context tar file is too large (>10MiB)")
			}

		}

		ctx := context.Background()
		job, err := getAPIClient().Submit(ctx, spec, deal, &buf)
		if err != nil {
			return err
		}

		log.Debug().Msgf(
			"submitting job with spec %+v", spec)

		cmd.Printf("%s\n", job.ID)
		return nil
	},
}

// from https://github.com/mimoo/eureka/blob/master/folders.go under Apache 2

//nolint:gocyclo
func compress(src string, buf io.Writer) error {
	// tar > gzip > buf
	zr := gzip.NewWriter(buf)
	tw := tar.NewWriter(zr)

	// is file a folder?
	fi, err := os.Stat(src)
	if err != nil {
		return err
	}
	mode := fi.Mode()
	if mode.IsRegular() {
		// get header
		var header *tar.Header
		header, err = tar.FileInfoHeader(fi, src)
		if err != nil {
			return err
		}
		// write header
		if err = tw.WriteHeader(header); err != nil { //nolint:gocritic
			return err
		}
		// get content
		var data *os.File
		data, err = os.Open(src)
		if err != nil {
			return err
		}
		if _, err = io.Copy(tw, data); err != nil {
			return err
		}
	} else if mode.IsDir() { // folder
		// walk through every file in the folder
		err = filepath.Walk(src, func(file string, fi os.FileInfo, _ error) error {
			// generate tar header
			var header *tar.Header
			header, err = tar.FileInfoHeader(fi, file)
			if err != nil {
				return err
			}

			// must provide real name
			// (see https://golang.org/src/archive/tar/common.go?#L626)
			header.Name = filepath.ToSlash(file)

			// write header
			if err = tw.WriteHeader(header); err != nil { //nolint:gocritic
				return err
			}
			// if not a dir, write file content
			if !fi.IsDir() {
				var data *os.File
				data, err = os.Open(file)
				if err != nil {
					return err
				}
				if _, err = io.Copy(tw, data); err != nil { //nolint:gocritic
					return err
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
	} else {
		return fmt.Errorf("error: file type not supported")
	}

	// produce tar
	if err := tw.Close(); err != nil {
		return err
	}
	// produce gzip
	if err := zr.Close(); err != nil {
		return err
	}
	//
	return nil
}
