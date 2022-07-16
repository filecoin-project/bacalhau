package devstack

import (
	"fmt"
	"io/ioutil"
	"strings"
	"testing"

	cmd "github.com/filecoin-project/bacalhau/cmd/bacalhau"
	"github.com/filecoin-project/bacalhau/pkg/computenode"
	"github.com/filecoin-project/bacalhau/pkg/devstack"
	"github.com/filecoin-project/bacalhau/pkg/executor"
	_ "github.com/filecoin-project/bacalhau/pkg/logger"
	"github.com/filecoin-project/bacalhau/pkg/publicapi"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/require"
)

func TestSimilarMoviesStdout(t *testing.T) {

	ctx, span := newSpan("TestSimilarMoviesStdout")
	defer span.End()
	stack, cm := SetupTest(t, 1, 0, computenode.NewDefaultComputeNodeConfig())
	defer TeardownTest(stack, cm)

	nodeIds, err := stack.GetNodeIds()
	require.NoError(t, err)

	_, out, err := cmd.ExecuteTestCobraCommand(t, cmd.RootCmd,
		fmt.Sprintf("--api-port=%d", stack.Nodes[0].APIServer.Port),
		"--api-host=localhost",
		"docker",
		"run",
		"jsace/python-similar-movies",
		"--",
		"/bin/bash",
		"-c",
		`'python similar-movies.py --k 50 --id 10 --n 1'`,
	)
	require.NoError(t, err)

	jobId := strings.TrimSpace(out)
	log.Debug().Msgf("jobId=%s", jobId)
	// wait for the job to complete across all nodes
	err = stack.WaitForJob(ctx, jobId,
		devstack.WaitForJobThrowErrors([]executor.JobStateType{
			executor.JobStateBidRejected,
			executor.JobStateError,
		}),
		devstack.WaitForJobAllHaveState(nodeIds, executor.JobStateComplete),
	)
	require.NoError(t, err)

	apiClient := publicapi.NewAPIClient(stack.Nodes[0].APIServer.GetURI())
	loadedJob, ok, err := apiClient.Get(ctx, jobId)
	require.True(t, ok)
	require.NoError(t, err)

	for nodeID, state := range loadedJob.State {
		node, err := stack.GetNode(ctx, nodeID)
		require.NoError(t, err)

		outputDir, err := ioutil.TempDir("", "bacalhau-ipfs-devstack-test")
		require.NoError(t, err)
		cid := state.ResultsID

		err = node.IpfsClient.Get(ctx, cid, outputDir)
		require.NoError(t, err)

		bytesDowloaded, err := ioutil.ReadFile(outputDir + "/" + cid + "/stdout")
		require.NoError(t, err)

		// read local files
		path_local := "../../testdata/python-similar-movies"
		bytesLocal, err := ioutil.ReadFile(path_local + "/" + cid + "/stdout")
		require.NoError(t, err)

		// compare the local file stored here
		log.Info().Msg(string(bytesDowloaded))
		require.Equal(t, bytesDowloaded, bytesLocal)
	}
}

func TestSyntheticDataGenerationOutputVolume(t *testing.T) {

	ctx, span := newSpan("TestSyntheticDataGenerationOutputVolume")
	defer span.End()
	stack, cm := SetupTest(t, 1, 0, computenode.NewDefaultComputeNodeConfig())
	defer TeardownTest(stack, cm)

	nodeIds, err := stack.GetNodeIds()
	require.NoError(t, err)

	_, out, err := cmd.ExecuteTestCobraCommand(t, cmd.RootCmd,
		fmt.Sprintf("--api-port=%d", stack.Nodes[0].APIServer.Port),
		"--api-host=localhost",
		"docker",
		"run",
		"-o output:/output",
		"jsace/synthetic-data-generation",
		"--",
		"/bin/bash",
		"-c",
		`'python datagen.py -n 5 -o ../output "01-01-2022" "02-01-2022"'`,
	)
	require.NoError(t, err)

	jobId := strings.TrimSpace(out)
	log.Debug().Msgf("jobId=%s", jobId)
	// wait for the job to complete across all nodes
	err = stack.WaitForJob(ctx, jobId,
		devstack.WaitForJobThrowErrors([]executor.JobStateType{
			executor.JobStateBidRejected,
			executor.JobStateError,
		}),
		devstack.WaitForJobAllHaveState(nodeIds, executor.JobStateComplete),
	)
	require.NoError(t, err)

	apiClient := publicapi.NewAPIClient(stack.Nodes[0].APIServer.GetURI())
	loadedJob, ok, err := apiClient.Get(ctx, jobId)
	require.True(t, ok)
	require.NoError(t, err)

	for nodeID, state := range loadedJob.State {

		node, err := stack.GetNode(ctx, nodeID)
		require.NoError(t, err)

		outputDir, err := ioutil.TempDir("", "bacalhau-ipfs-devstack-test")
		require.NoError(t, err)
		cid := state.ResultsID

		err = node.IpfsClient.Get(ctx, cid, outputDir)
		require.NoError(t, err)

		bytesDowloaded, err := ioutil.ReadFile(outputDir + "/" + cid + "/stdout")
		require.NoError(t, err)

		// read local files
		path_local := "../../testdata/synthetic-data-generation"
		bytesLocal, err := ioutil.ReadFile(path_local + "/" + "QmWyQ4hDPzjX271utC8WsYY5XodRk9RKmEDHMj24KYDA9S" + "/stdout")
		require.NoError(t, err)

		// compare the local file stored here
		log.Info().Msg(string(bytesDowloaded))
		require.Equal(t, bytesDowloaded, bytesLocal)
	}

}

func TestCoresetInputVolumeStdout(t *testing.T) {
	nodeCount := 3

	ctx, span := newSpan("TestCoresetInputVolumeStdout")
	defer span.End()
	stack, cm := SetupTest(t, 1, 0, computenode.NewDefaultComputeNodeConfig())
	defer TeardownTest(stack, cm)

	nodeIds, err := stack.GetNodeIds()
	require.NoError(t, err)

	// upload input files to local IPFS
	filePath := "../../testdata/coreset/liechtenstein-latest/liechtenstein-latest.osm.pbf"
	fileCid, err := stack.AddFileToNodes(nodeCount, filePath)
	require.NoError(t, err)

	_, out, err := cmd.ExecuteTestCobraCommand(t, cmd.RootCmd,
		fmt.Sprintf("--api-port=%d", stack.Nodes[0].APIServer.Port),
		"--api-host=localhost",
		"docker",
		"run",
		fmt.Sprintf("-v %s:/input", fileCid),
		"jsace/coreset",
		"--",
		"/bin/bash",
		"-c",
		`'osmium export input/liechtenstein-latest.osm.pbf -o liechtenstein-latest.geojson;python Coreset/python/coreset.py -f liechtenstein-latest.geojson'`,
	)
	require.NoError(t, err)

	jobId := strings.TrimSpace(out)
	log.Debug().Msgf("jobId=%s", jobId)
	// wait for the job to complete across all nodes
	err = stack.WaitForJob(ctx, jobId,
		devstack.WaitForJobThrowErrors([]executor.JobStateType{
			executor.JobStateBidRejected,
			executor.JobStateError,
		}),
		devstack.WaitForJobAllHaveState(nodeIds, executor.JobStateComplete),
	)
	require.NoError(t, err)

	// since it's non deterministic rewrite it in WASM
	// 	apiClient := publicapi.NewAPIClient(stack.Nodes[0].APIServer.GetURI())
	// loadedJob, ok, err := apiClient.Get(ctx, jobId)
	// require.True(t, ok)
	// require.NoError(t, err)

	// for nodeID, state := range loadedJob.State {

	// 	outputDir, err := ioutil.TempDir("", "bacalhau-ipfs-devstack-test")
	// 	require.NoError(t, err)
	// 	cid := state.ResultsID
	// 	err = node.IpfsClient.Get(ctx, cid, outputDir)
	// 	require.NoError(t, err)

	// 	bytesDowloaded, err := ioutil.ReadFile(outputDir + "/" + cid + "/stdout")
	// 	require.NoError(err)

	// 	// read local files
	// 	path_local := "../../testdata/coreset"
	// 	bytesLocal, err := ioutil.ReadFile(path_local + "/" + cid + "/stdout")
	// 	require.NoError(err)

	// 	// compare the local file stored here
	// 	log.Info().Msg(string(bytesDowloaded))
	// 	require.Equal(t, bytesDowloaded, bytesLocal)
	// }

}

func TestGROMACSInputVolumeOutputVolume(t *testing.T) {
	nodeCount := 3

	ctx, span := newSpan("TestGMORACSInputVolumeOutputVolume")
	defer span.End()
	stack, cm := SetupTest(t, 1, 0, computenode.NewDefaultComputeNodeConfig())
	defer TeardownTest(stack, cm)

	nodeIds, err := stack.GetNodeIds()
	require.NoError(t, err)

	// upload input files to local IPFS
	filePath := "../../testdata/GROMACS/input/1AKI.pdb"
	fileCid, err := stack.AddFileToNodes(nodeCount, filePath)
	require.NoError(t, err)

	_, out, err := cmd.ExecuteTestCobraCommand(t, cmd.RootCmd,
		fmt.Sprintf("--api-port=%d", stack.Nodes[0].APIServer.Port),
		"--api-host=localhost",
		"docker",
		"run",
		fmt.Sprintf("-v %s:/input", fileCid),
		"-o output:/output",
		"gromacs/gromacs",
		"--",
		"/bin/bash",
		"-c",
		`'echo 15 | gmx pdb2gmx -f input/1AKI.pdb -o output/1AKI_processed.gro -water spc'`,
	)
	require.NoError(t, err)

	jobId := strings.TrimSpace(out)
	log.Debug().Msgf("jobId=%s", jobId)
	// wait for the job to complete across all nodes
	err = stack.WaitForJob(ctx, jobId,
		devstack.WaitForJobThrowErrors([]executor.JobStateType{
			executor.JobStateBidRejected,
			executor.JobStateError,
		}),
		devstack.WaitForJobAllHaveState(nodeIds, executor.JobStateComplete),
	)
	require.NoError(t, err)

	// load result from ipfs and check it

	apiClient := publicapi.NewAPIClient(stack.Nodes[0].APIServer.GetURI())
	loadedJob, ok, err := apiClient.Get(ctx, jobId)
	require.True(t, ok)
	require.NoError(t, err)

	for nodeID, state := range loadedJob.State {

		node, err := stack.GetNode(ctx, nodeID)
		require.NoError(t, err)

		outputDir, err := ioutil.TempDir("", "bacalhau-ipfs-devstack-test")
		require.NoError(t, err)
		cid := state.ResultsID

		err = node.IpfsClient.Get(ctx, cid, outputDir)
		require.NoError(t, err)

		bytesDowloaded, err := ioutil.ReadFile(outputDir + "/" + cid + "/output" + "/1AKI_processed.gro")
		require.NoError(t, err)

		// read local files
		path_local := "../../testdata/synthetic-data-generation"
		bytesLocal, err := ioutil.ReadFile(path_local + "/" + "QmVZKeKAsKswY4uXcBkUz9eo19Qy9FNF7hUXnoxEZTGTM8" + "/output" + "/1AKI_processed.gro")
		require.NoError(t, err)

		// compare the local file stored here
		log.Info().Msg(string(bytesDowloaded))
		require.Equal(t, bytesDowloaded, bytesLocal)
	}

}
