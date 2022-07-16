package devstack

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/filecoin-project/bacalhau/pkg/computenode"
	"github.com/filecoin-project/bacalhau/pkg/devstack"
	"github.com/filecoin-project/bacalhau/pkg/executor"
	executor_util "github.com/filecoin-project/bacalhau/pkg/executor/util"
	_ "github.com/filecoin-project/bacalhau/pkg/logger"
	"github.com/filecoin-project/bacalhau/pkg/storage"
	"github.com/filecoin-project/bacalhau/pkg/system"
	"github.com/filecoin-project/bacalhau/pkg/verifier"
	verifier_util "github.com/filecoin-project/bacalhau/pkg/verifier/util"
	"github.com/stretchr/testify/require"
)

var StorageDriverNames = []string{
	storage.IPFSFuseDocker,
	storage.IPFSAPICopy,
}

func SetupTest(
	t *testing.T,
	nodes int, badActors int,
	//nolint:gocritic
	config computenode.ComputeNodeConfig,
) (*devstack.DevStack, *system.CleanupManager) {
	system.InitConfigForTesting(t)

	cm := system.NewCleanupManager()
	getExecutors := func(ipfsMultiAddress string, nodeIndex int) (map[executor.EngineType]executor.Executor, error) {
		fmt.Printf("-----> IPFS ADDRESS: %s\n", ipfsMultiAddress)
		ipfsParts := strings.Split(ipfsMultiAddress, "/")
		ipfsSuffix := ipfsParts[len(ipfsParts)-1]
		return executor_util.NewStandardExecutors(
			cm, ipfsMultiAddress, fmt.Sprintf("devstacknode%d-%s", nodeIndex, ipfsSuffix))
	}
	getVerifiers := func(ipfsMultiAddress string, nodeIndex int) (map[verifier.VerifierType]verifier.Verifier, error) {
		fmt.Printf("-----> IPFS ADDRESS: %s\n", ipfsMultiAddress)
		return verifier_util.NewIPFSVerifiers(cm, ipfsMultiAddress)
	}
	stack, err := devstack.NewDevStack(
		cm,
		nodes,
		badActors,
		getExecutors,
		getVerifiers,
		config,
	)
	require.NoError(t, err)

	// important to give the pubsub network time to connect
	time.Sleep(time.Second)

	return stack, cm
}

func TeardownTest(stack *devstack.DevStack, cm *system.CleanupManager) {
	stack.PrintNodeInfo()
	cm.Cleanup()
}
