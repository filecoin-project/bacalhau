package devstack

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/filecoin-project/bacalhau/pkg/computenode"
	"github.com/filecoin-project/bacalhau/pkg/devstack"
	"github.com/filecoin-project/bacalhau/pkg/executor"
	_ "github.com/filecoin-project/bacalhau/pkg/logger"
	"github.com/filecoin-project/bacalhau/pkg/publicapi"
	"github.com/filecoin-project/bacalhau/pkg/verifier"
	"github.com/stretchr/testify/require"
)

func TestErrorContainer(t *testing.T) {

	stdout := "apples"
	stderr := "oranges"
	exitCode := "19"

	ctx, span := newSpan("TestErrorContainer")
	defer span.End()

	stack, cm := SetupTest(
		t,
		1,
		0,
		computenode.NewDefaultComputeNodeConfig(),
	)
	defer TeardownTest(stack, cm)

	nodeIDs, err := stack.GetNodeIds()
	require.NoError(t, err)

	jobSpec := &executor.JobSpec{
		Engine:   executor.EngineDocker,
		Verifier: verifier.VerifierIpfs,
		Docker: executor.JobSpecDocker{
			Image: "ubuntu",
			Entrypoint: []string{
				"bash",
				"-c",
				fmt.Sprintf("echo %s && >&2 echo %s && exit %s", stdout, stderr, exitCode),
			},
		},
	}

	jobDeal := &executor.JobDeal{
		Concurrency: 1,
	}

	apiUri := stack.Nodes[0].APIServer.GetURI()
	apiClient := publicapi.NewAPIClient(apiUri)
	submittedJob, err := apiClient.Submit(ctx, jobSpec, jobDeal, nil)
	require.NoError(t, err)

	// wait for the job to complete across all nodes
	err = stack.WaitForJob(ctx, submittedJob.ID,
		devstack.WaitForJobThrowErrors([]executor.JobStateType{
			executor.JobStateBidRejected,
			executor.JobStateComplete,
		}),
		devstack.WaitForJobAllHaveState(nodeIDs, executor.JobStateError),
	)
	require.NoError(t, err)

	loadedJob, ok, err := apiClient.Get(ctx, submittedJob.ID)
	require.True(t, ok)
	require.NoError(t, err)

	state, ok := loadedJob.State[nodeIDs[0]]
	require.True(t, ok)

	outputDir, err := ioutil.TempDir("", "bacalhau-ipfs-devstack-test")
	require.NoError(t, err)

	node, err := stack.GetNode(ctx, nodeIDs[0])
	require.NoError(t, err)

	outputPath := filepath.Join(outputDir, state.ResultsID)
	err = node.IpfsClient.Get(ctx, state.ResultsID, outputPath)
	require.NoError(t, err)

	stdoutBytes, err := os.ReadFile(fmt.Sprintf("%s/stdout", outputPath))
	require.NoError(t, err)
	stderrBytes, err := os.ReadFile(fmt.Sprintf("%s/stderr", outputPath))
	require.NoError(t, err)
	exitCodeBytes, err := os.ReadFile(fmt.Sprintf("%s/exitCode", outputPath))
	require.NoError(t, err)

	require.Equal(t, stdout, strings.TrimSpace(string(stdoutBytes)))
	require.Equal(t, stderr, strings.TrimSpace(string(stderrBytes)))
	require.Equal(t, exitCode, strings.TrimSpace(string(exitCodeBytes)))
}
