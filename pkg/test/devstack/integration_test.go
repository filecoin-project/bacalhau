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
	"github.com/filecoin-project/bacalhau/pkg/system"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// Define the suite, and absorb the built-in basic suite
// functionality from testify - including a T() method which
// returns the current testing context
type IntegrationSuite struct {
	suite.Suite
	// rootCmd *cobra.Command
}

// Before all suite
func (suite *IntegrationSuite) SetupAllSuite() {

}

// Before each test
func (suite *IntegrationSuite) SetupTest() {
	system.InitConfigForTesting(suite.T())
	// suite.rootCmd = RootCmd
}

func (suite *IntegrationSuite) TearDownTest() {
}

func (suite *IntegrationSuite) TearDownAllSuite() {

}

func (suite *IntegrationSuite) TestSimplePython() {
	nodeCount := 3

	ctx, span := newSpan("TestSimplePython")
	defer span.End()
	stack, cm := SetupTest(suite.T(), nodeCount, 0, computenode.NewDefaultComputeNodeConfig())
	defer TeardownTest(stack, cm)

	nodeIds, err := stack.GetNodeIds()
	require.NoError(suite.T(), err)

	_, out, err := cmd.ExecuteTestCobraCommand(suite.T(), cmd.RootCmd,
		fmt.Sprintf("--api-port=%d", stack.Nodes[0].APIServer.Port),
		"--api-host=localhost",
		"docker",
		"run",
		"python",
		"--",
		"/bin/bash",
		"-c",
		`python -c 'print(1+1)'`,
	)
	require.NoError(suite.T(), err)

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
	require.NoError(suite.T(), err)
}

func (suite *IntegrationSuite) TestSimilarMoviesStdout() {
	nodeCount := 1

	ctx, span := newSpan("TestSimilarMoviesStdout")
	defer span.End()
	stack, cm := SetupTest(suite.T(), nodeCount, 0, computenode.NewDefaultComputeNodeConfig())
	defer TeardownTest(stack, cm)

	nodeIds, err := stack.GetNodeIds()
	require.NoError(suite.T(), err)

	// bacalhau docker run jsace/python-similar-movies -- /bin/bash -c 'python similar-movies.py --k 50 --id 10 --n 1'

	_, out, err := cmd.ExecuteTestCobraCommand(suite.T(), cmd.RootCmd,
		fmt.Sprintf("--api-port=%d", stack.Nodes[0].APIServer.Port),
		"--api-host=localhost",
		"docker",
		"run",
		"jsace/python-similar-movies",
		"--",
		"/bin/bash",
		"-c",
		`python similar-movies.py --k 50 --id 10 --n 1`,
	)
	// `'python similar-movies.py --k 50 --id 10 --n 1'`,
	require.NoError(suite.T(), err)

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
	require.NoError(suite.T(), err)

	// apiClient := publicapi.NewAPIClient(stack.Nodes[0].APIServer.GetURI())
	// loadedJob, ok, err := apiClient.Get(ctx, jobId)
	// require.True(suite.T(), ok)
	// require.NoError(suite.T(), err)

	// for nodeID, state := range loadedJob.State {
	// 	node, err := stack.GetNode(ctx, nodeID)
	// 	require.NoError(suite.T(), err)

	// 	outputDir, err := ioutil.TempDir("", "bacalhau-ipfs-devstack-test")
	// 	require.NoError(suite.T(), err)
	// 	cid := state.ResultsID

	// 	err = node.IpfsClient.Get(ctx, cid, outputDir)
	// 	require.NoError(suite.T(), err)

	// 	bytesDowloaded, err := ioutil.ReadFile(outputDir + "/" + cid + "/stdout")
	// 	require.NoError(suite.T(), err)

	// 	// read local files
	// 	path_local := "../../../testdata/python-similar-movies"
	// 	bytesLocal, err := ioutil.ReadFile(path_local + "/" + cid + "/stdout")
	// 	require.NoError(suite.T(), err)

	// 	// compare the local file stored here
	// 	log.Info().Msg(string(bytesDowloaded))
	// 	require.Equal(suite.T(), bytesDowloaded, bytesLocal)
	// }
}

func (suite *IntegrationSuite) TestSyntheticDataGenerationOutputVolume() {

	ctx, span := newSpan("TestSyntheticDataGenerationOutputVolume")
	defer span.End()
	stack, cm := SetupTest(suite.T(), 1, 0, computenode.NewDefaultComputeNodeConfig())
	defer TeardownTest(stack, cm)

	nodeIds, err := stack.GetNodeIds()
	require.NoError(suite.T(), err)

	_, out, err := cmd.ExecuteTestCobraCommand(suite.T(), cmd.RootCmd,
		fmt.Sprintf("--api-port=%d", stack.Nodes[0].APIServer.Port),
		"--api-host=localhost",
		"docker",
		"run",
		"-o output:/output",
		"jsace/synthetic-data-generation",
		"--",
		"/bin/bash",
		"-c",
		`python datagen.py -n 5 -o ../output "01-01-2022" "02-01-2022"`,
	)
	require.NoError(suite.T(), err)

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
	require.NoError(suite.T(), err)

	// apiClient := publicapi.NewAPIClient(stack.Nodes[0].APIServer.GetURI())
	// loadedJob, ok, err := apiClient.Get(ctx, jobId)
	// require.True(suite.T(), ok)
	// require.NoError(suite.T(), err)

	// for nodeID, state := range loadedJob.State {

	// 	node, err := stack.GetNode(ctx, nodeID)
	// 	require.NoError(suite.T(), err)

	// 	outputDir, err := ioutil.TempDir("", "bacalhau-ipfs-devstack-test")
	// 	require.NoError(suite.T(), err)
	// 	cid := state.ResultsID

	// 	err = node.IpfsClient.Get(ctx, cid, outputDir)
	// 	require.NoError(suite.T(), err)

	// 	bytesDowloaded, err := ioutil.ReadFile(outputDir + "/" + cid + "/stdout")
	// 	require.NoError(suite.T(), err)

	// 	// read local files
	// 	path_local := "../../../testdata/synthetic-data-generation"
	// 	bytesLocal, err := ioutil.ReadFile(path_local + "/" + "QmWyQ4hDPzjX271utC8WsYY5XodRk9RKmEDHMj24KYDA9S" + "/stdout")
	// 	require.NoError(suite.T(), err)

	// 	// compare the local file stored here
	// 	log.Info().Msg(string(bytesDowloaded))
	// 	require.Equal(suite.T(), bytesDowloaded, bytesLocal)
	// }

}

func (suite *IntegrationSuite) TestCoresetInputVolumeStdout() {
	nodeCount := 1

	ctx, span := newSpan("TestCoresetInputVolumeStdout")
	defer span.End()
	stack, cm := SetupTest(suite.T(), nodeCount, 0, computenode.NewDefaultComputeNodeConfig())
	defer TeardownTest(stack, cm)

	nodeIds, err := stack.GetNodeIds()
	require.NoError(suite.T(), err)

	// upload input files to local IPFS
	filePath := "../../../testdata/coreset/liechtenstein-latest/liechtenstein-latest.osm.pbf"
	fileCid, err := stack.AddFileToNodes(nodeCount, filePath)
	fmt.Printf("-v %s:/input", fileCid)
	require.NoError(suite.T(), err)

	_, out, err := cmd.ExecuteTestCobraCommand(suite.T(), cmd.RootCmd,
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
	require.NoError(suite.T(), err)

	// `python Coreset/python/coreset.py -f monaco-latest.geojson`,
	require.NoError(suite.T(), err)

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
	require.NoError(suite.T(), err)

	// since it's non deterministic rewrite it in WASM
	// 	apiClient := publicapi.NewAPIClient(stack.Nodes[0].APIServer.GetURI())
	// loadedJob, ok, err := apiClient.Get(ctx, jobId)
	// require.True(suite.T(), ok)
	// require.NoError(suite.T(), err)

	// for nodeID, state := range loadedJob.State {

	// 	outputDir, err := ioutil.TempDir("", "bacalhau-ipfs-devstack-test")
	// 	require.NoError(suite.T(), err)
	// 	cid := state.ResultsID
	// 	err = node.IpfsClient.Get(ctx, cid, outputDir)
	// 	require.NoError(suite.T(), err)

	// 	bytesDowloaded, err := ioutil.ReadFile(outputDir + "/" + cid + "/stdout")
	// 	require.NoError(err)

	// 	// read local files
	// 	path_local := "../../testdata/coreset"
	// 	bytesLocal, err := ioutil.ReadFile(path_local + "/" + cid + "/stdout")
	// 	require.NoError(err)

	// 	// compare the local file stored here
	// 	log.Info().Msg(string(bytesDowloaded))
	// 	require.Equal(suite.T(), bytesDowloaded, bytesLocal)
	// }

}

func (suite *IntegrationSuite) TestGROMACSInputVolumeOutputVolume() {
	nodeCount := 1

	ctx, span := newSpan("TestGMORACSInputVolumeOutputVolume")
	defer span.End()
	stack, cm := SetupTest(suite.T(), 1, 0, computenode.NewDefaultComputeNodeConfig())
	defer TeardownTest(stack, cm)

	nodeIds, err := stack.GetNodeIds()
	require.NoError(suite.T(), err)

	// upload input files to local IPFS
	filePath := "../../../testdata/GROMACS/input/1AKI.pdb"
	fileCid, err := stack.AddFileToNodes(nodeCount, filePath)
	require.NoError(suite.T(), err)

	_, out, err := cmd.ExecuteTestCobraCommand(suite.T(), cmd.RootCmd,
		fmt.Sprintf("--api-port=%d", stack.Nodes[0].APIServer.Port),
		"--api-host=localhost",
		"docker",
		"run",
		fmt.Sprintf("-v %s:/input", strings.TrimSpace(fileCid)),
		"-o output:/output",
		"gromacs/gromacs",
		"--",
		"/bin/bash",
		"-c",
		`'echo 15 | gmx pdb2gmx -f input/1AKI.pdb -o output/1AKI_processed.gro -water spc'`,
	)
	require.NoError(suite.T(), err)

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
	require.NoError(suite.T(), err)

	// load result from ipfs and check it

	apiClient := publicapi.NewAPIClient(stack.Nodes[0].APIServer.GetURI())
	loadedJob, ok, err := apiClient.Get(ctx, jobId)
	require.True(suite.T(), ok)
	require.NoError(suite.T(), err)

	for nodeID, state := range loadedJob.State {

		node, err := stack.GetNode(ctx, nodeID)
		require.NoError(suite.T(), err)

		outputDir, err := ioutil.TempDir("", "bacalhau-ipfs-devstack-test")
		require.NoError(suite.T(), err)
		cid := state.ResultsID

		err = node.IpfsClient.Get(ctx, cid, outputDir)
		require.NoError(suite.T(), err)

		bytesDowloaded, err := ioutil.ReadFile(outputDir + "/" + cid + "/output" + "/1AKI_processed.gro")
		require.NoError(suite.T(), err)

		// read local files
		path_local := "../../../testdata/synthetic-data-generation"
		bytesLocal, err := ioutil.ReadFile(path_local + "/" + "QmVZKeKAsKswY4uXcBkUz9eo19Qy9FNF7hUXnoxEZTGTM8" + "/output" + "/1AKI_processed.gro")
		require.NoError(suite.T(), err)

		// compare the local file stored here
		log.Info().Msg(string(bytesDowloaded))
		require.Equal(suite.T(), bytesDowloaded, bytesLocal)
	}

}

// In order for 'go test' to run this suite, we need to create
// a normal test function and pass our suite to suite.Run
func TestIntegrationSuite(t *testing.T) {
	suite.Run(t, new(IntegrationSuite))
}
