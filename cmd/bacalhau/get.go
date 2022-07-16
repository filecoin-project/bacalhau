package bacalhau

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/filecoin-project/bacalhau/pkg/ipfs"
	"github.com/filecoin-project/bacalhau/pkg/system"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var getCmdFlags = struct {
	timeoutSecs    int
	outputDir      string
	ipfsSwarmAddrs string
}{
	timeoutSecs:    10,
	outputDir:      ".",
	ipfsSwarmAddrs: strings.Join(system.Envs[system.Production].IPFSSwarmAddresses, ","),
}

func init() { // nolint:gochecknoinits
	getCmd.Flags().IntVar(&getCmdFlags.timeoutSecs, "timeout-secs",
		getCmdFlags.timeoutSecs, "Timeout duration for IPFS downloads.")
	getCmd.Flags().StringVar(&getCmdFlags.outputDir, "output-dir",
		getCmdFlags.outputDir, "Directory to write the output to.")
	getCmd.Flags().StringVar(&getCmdFlags.ipfsSwarmAddrs, "ipfs-swarm-addrs",
		getCmdFlags.ipfsSwarmAddrs, "Comma-separated list of IPFS nodes to connect to.")
}

var getCmd = &cobra.Command{
	Use:   "get",
	Short: "Get the results of a job",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cm := system.NewCleanupManager()
		defer cm.Cleanup()

		log.Info().Msgf("Fetching results of job '%s'...", args[0])
		job, ok, err := getAPIClient().Get(context.Background(), args[0])
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("job not found")
		}

		resultCIDs := map[string]bool{}
		for _, jobState := range job.State {
			if jobState.ResultsID != "" {
				resultCIDs[jobState.ResultsID] = true
			}
		}
		log.Debug().Msgf("Job has result CIDs: %v", resultCIDs)

		if len(resultCIDs) == 0 {
			log.Info().Msg("Job has no results.")
			return nil
		}

		swarmAddrs := []string{}
		if getCmdFlags.ipfsSwarmAddrs != "" {
			swarmAddrs = strings.Split(getCmdFlags.ipfsSwarmAddrs, ",")
		}

		// NOTE: we have to spin up a temporary IPFS node as we don't
		// generally have direct access to a remote node's API server.
		log.Debug().Msg("Spinning up IPFS node...")
		n, err := ipfs.NewNode(cm, swarmAddrs)
		if err != nil {
			return err
		}

		log.Debug().Msg("Connecting client to new IPFS node...")
		cl, err := n.Client()
		if err != nil {
			return err
		}

		// NOTE: this will run in non-deterministic order
		for cid := range resultCIDs {
			outputDir := filepath.Join(getCmdFlags.outputDir, cid)
			ok, err := system.PathExists(outputDir)
			if err != nil {
				return err
			}
			if ok {
				log.Warn().Msgf("Output directory '%s' already exists, skipping CID '%s'.", outputDir, cid)
				continue
			}

			log.Info().Msgf("Downloading result CID '%s' to '%s'...",
				cid, outputDir)

			ctx, cancel := context.WithDeadline(context.Background(),
				time.Now().Add(time.Second*time.Duration(getCmdFlags.timeoutSecs)))
			defer cancel()

			err = cl.Get(ctx, cid, outputDir)
			if err != nil {
				if errors.Is(err, context.DeadlineExceeded) {
					log.Error().Msg("Timed out while downloading result.")
				}

				return err
			}
		}

		return nil
	},
}
