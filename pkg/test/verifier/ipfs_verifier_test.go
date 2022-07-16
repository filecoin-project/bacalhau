package verifier

import (
	"context"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/filecoin-project/bacalhau/pkg/logger"
	"github.com/filecoin-project/bacalhau/pkg/verifier/ipfs"
	"github.com/stretchr/testify/require"
)

func TestIPFSVerifier(t *testing.T) {
	ctx := context.Background()
	stack, cm := SetupTest(t, 1)
	defer TeardownTest(stack, cm)

	inputDir, err := ioutil.TempDir("", "bacalhau-ipfs-verifier-test")
	require.NoError(t, err)

	outputDir, err := ioutil.TempDir("", "bacalhau-ipfs-verifier-test")
	require.NoError(t, err)

	fixtureContent := "hello world"
	err = os.WriteFile(inputDir+"/file.txt", []byte(fixtureContent), 0644)
	require.NoError(t, err)

	verifier, err := ipfs.NewVerifier(
		cm, stack.Nodes[0].IpfsClient.APIAddress())
	require.NoError(t, err)

	installed, err := verifier.IsInstalled(ctx)
	require.NoError(t, err)
	require.True(t, installed)

	resultHash, err := verifier.ProcessResultsFolder(ctx,
		"fake-job-id", inputDir)
	require.NoError(t, err)

	outputPath := filepath.Join(outputDir, resultHash)
	err = verifier.IPFSClient.Get(ctx, resultHash, outputPath)
	require.NoError(t, err)

	outputContent, err := os.ReadFile(outputPath + "/file.txt")
	require.NoError(t, err)

	require.Equal(t, fixtureContent, string(outputContent))
}
