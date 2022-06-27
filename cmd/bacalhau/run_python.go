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
	"strings"

	"github.com/filecoin-project/bacalhau/pkg/job"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var deterministic bool
var command string
var requirementsPath string
var contextPath string

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
		&contextPath, "context-path", ".", // TODO: consider replacing this with context-glob, default to "./**/*.py|./requirements.txt"
		"Path to context (e.g. python code) to send to server (via public IPFS network) for execution (max 10MiB). Set to empty string to disable",
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
			[]string{}, // no input volumes (yet)
			[]string{}, // no output volumes (yet)
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
		if contextPath != "" {
			// construct a tar file from the contextPath directory
			// tar + gzip
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

		fmt.Printf("%s\n", job.Id)
		return nil
	},
}

// from https://github.com/mimoo/eureka/blob/master/folders.go under Apache 2

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
		header, err := tar.FileInfoHeader(fi, src)
		if err != nil {
			return err
		}
		// write header
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		// get content
		data, err := os.Open(src)
		if err != nil {
			return err
		}
		if _, err := io.Copy(tw, data); err != nil {
			return err
		}
	} else if mode.IsDir() { // folder

		// walk through every file in the folder
		filepath.Walk(src, func(file string, fi os.FileInfo, err error) error {
			// generate tar header
			header, err := tar.FileInfoHeader(fi, file)
			if err != nil {
				return err
			}

			// must provide real name
			// (see https://golang.org/src/archive/tar/common.go?#L626)
			header.Name = filepath.ToSlash(file)

			// write header
			if err := tw.WriteHeader(header); err != nil {
				return err
			}
			// if not a dir, write file content
			if !fi.IsDir() {
				data, err := os.Open(file)
				if err != nil {
					return err
				}
				if _, err := io.Copy(tw, data); err != nil {
					return err
				}
			}
			return nil
		})
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

// check for path traversal and correct forward slashes
func validRelPath(p string) bool {
	if p == "" || strings.Contains(p, `\`) || strings.HasPrefix(p, "/") || strings.Contains(p, "../") {
		return false
	}
	return true
}

func decompress(src io.Reader, dst string) error {
	// ungzip
	zr, err := gzip.NewReader(src)
	if err != nil {
		return err
	}
	// untar
	tr := tar.NewReader(zr)

	// uncompress each element
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break // End of archive
		}
		if err != nil {
			return err
		}
		target := header.Name

		// validate name against path traversal
		if !validRelPath(header.Name) {
			return fmt.Errorf("tar contained invalid name error %q", target)
		}

		// add dst + re-format slashes according to system
		target = filepath.Join(dst, header.Name)
		// if no join is needed, replace with ToSlash:
		// target = filepath.ToSlash(header.Name)

		// check the type
		switch header.Typeflag {

		// if its a dir and it doesn't exist create it (with 0755 permission)
		case tar.TypeDir:
			if _, err := os.Stat(target); err != nil {
				if err := os.MkdirAll(target, 0755); err != nil {
					return err
				}
			}
		// if it's a file create it (with same permission)
		case tar.TypeReg:
			fileToWrite, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR, os.FileMode(header.Mode))
			if err != nil {
				return err
			}
			// copy over contents
			if _, err := io.Copy(fileToWrite, tr); err != nil {
				return err
			}
			// manually close here after each file operation; defering would cause each file close
			// to wait until all operations have completed.
			fileToWrite.Close()
		}
	}

	//
	return nil
}