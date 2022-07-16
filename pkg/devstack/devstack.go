package devstack

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/filecoin-project/bacalhau/pkg/computenode"
	"github.com/filecoin-project/bacalhau/pkg/config"
	"github.com/filecoin-project/bacalhau/pkg/executor"
	"github.com/filecoin-project/bacalhau/pkg/ipfs"
	"github.com/filecoin-project/bacalhau/pkg/publicapi"
	"github.com/filecoin-project/bacalhau/pkg/requestornode"
	"github.com/filecoin-project/bacalhau/pkg/storage/util"
	"github.com/filecoin-project/bacalhau/pkg/system"
	"github.com/filecoin-project/bacalhau/pkg/transport/libp2p"
	"github.com/filecoin-project/bacalhau/pkg/verifier"
	"github.com/phayes/freeport"
	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel/trace"
)

type DevStackNode struct {
	ComputeNode   *computenode.ComputeNode
	RequesterNode *requestornode.RequesterNode
	Transport     *libp2p.Transport

	IpfsNode   *ipfs.Node
	IpfsClient *ipfs.Client
	APIServer  *publicapi.APIServer
}

type DevStack struct {
	Nodes []*DevStackNode
}

type GetExecutorsFunc func(ipfsMultiAddress string, nodeIndex int) (
	map[executor.EngineType]executor.Executor, error)

type GetVerifiersFunc func(ipfsMultiAddress string, nodeIndex int) (
	map[verifier.VerifierType]verifier.Verifier, error)

//nolint:funlen,gocyclo
func NewDevStack(
	cm *system.CleanupManager,
	count, badActors int, // nolint:unparam // Incorrectly assumed as unused
	getExecutors GetExecutorsFunc,
	getVerifiers GetVerifiersFunc,
	//nolint:gocritic
	config computenode.ComputeNodeConfig,
) (*DevStack, error) {
	ctx, span := newSpan("NewDevStack")
	defer span.End()

	nodes := []*DevStackNode{}
	for i := 0; i < count; i++ {
		log.Debug().Msgf(`Creating Node #%d`, i)

		//////////////////////////////////////
		// IPFS
		//////////////////////////////////////
		var err error
		var ipfsSwarmAddrs []string
		if i > 0 {
			ipfsSwarmAddrs, err = nodes[0].IpfsNode.SwarmAddresses()
			if err != nil {
				return nil, fmt.Errorf("failed to get ipfs swarm addresses: %w", err)
			}
		}

		ipfsNode, err := ipfs.NewLocalNode(cm, ipfsSwarmAddrs)
		if err != nil {
			return nil, fmt.Errorf("failed to create ipfs node: %w", err)
		}

		ipfsClient, err := ipfsNode.Client()
		if err != nil {
			return nil, fmt.Errorf("failed to create ipfs client: %w", err)
		}

		ipfsAPIAddrs, err := ipfsNode.APIAddresses()
		if err != nil {
			return nil, fmt.Errorf("failed to get ipfs api addresses: %w", err)
		}
		if len(ipfsAPIAddrs) == 0 { // should never happen
			return nil, fmt.Errorf("devstack ipfs node has no api addresses")
		}

		//////////////////////////////////////
		// Scheduler
		//////////////////////////////////////
		libp2pPort, err := freeport.GetFreePort()
		if err != nil {
			return nil, err
		}

		transport, err := libp2p.NewTransport(cm, libp2pPort)
		if err != nil {
			return nil, err
		}

		//////////////////////////////////////
		// Executors and verifiers
		//////////////////////////////////////
		executors, err := getExecutors(ipfsAPIAddrs[0], i)
		if err != nil {
			return nil, err
		}

		verifiers, err := getVerifiers(ipfsAPIAddrs[0], i)
		if err != nil {
			return nil, err
		}

		//////////////////////////////////////
		// Requestor node
		//////////////////////////////////////
		requesterNode, err := requestornode.NewRequesterNode(
			cm,
			transport,
			verifiers,
		)
		if err != nil {
			return nil, err
		}

		//////////////////////////////////////
		// Compute node
		//////////////////////////////////////
		computeNode, err := computenode.NewComputeNode(
			cm,
			transport,
			executors,
			verifiers,
			config,
		)
		if err != nil {
			return nil, err
		}

		//////////////////////////////////////
		// JSON RPC
		//////////////////////////////////////

		apiPort, err := freeport.GetFreePort()
		if err != nil {
			return nil, err
		}

		apiServer := publicapi.NewServer(requesterNode, "0.0.0.0", apiPort)
		go func(ctx context.Context) {
			var gerr error // don't capture outer scope
			if gerr = apiServer.ListenAndServe(ctx, cm); gerr != nil {
				panic(err) // if api server can't run, devstack should stop
			}
		}(context.Background())

		log.Debug().Msgf("public API server started: 0.0.0.0:%d", apiPort)

		//////////////////////////////////////
		// metrics
		//////////////////////////////////////

		metricsPort, err := freeport.GetFreePort()
		if err != nil {
			return nil, err
		}

		go func(ctx context.Context) {
			var gerr error // don't capture outer scope
			if gerr = system.ListenAndServeMetrics(cm, metricsPort); gerr != nil {
				log.Error().Msgf("Cannot serve metrics: %v", err)
			}
		}(context.Background())

		//////////////////////////////////////
		// intra-connections
		//////////////////////////////////////

		go func(ctx context.Context) {
			if err = transport.Start(ctx); err != nil {
				panic(err) // if transport can't run, devstack should stop
			}
		}(context.Background())

		log.Debug().Msgf("libp2p server started: %d", libp2pPort)

		if len(nodes) > 0 {
			// connect the libp2p scheduler node
			firstNode := nodes[0]

			// get the libp2p id of the first scheduler node
			libp2pHostID, err := firstNode.Transport.HostID(ctx)
			if err != nil {
				return nil, err
			}

			// connect this scheduler to the first
			firstSchedulerAddress := fmt.Sprintf("/ip4/127.0.0.1/tcp/%d/p2p/%s", firstNode.Transport.Port, libp2pHostID)
			log.Debug().Msgf("Connect to first libp2p scheduler node: %s", firstSchedulerAddress)
			err = transport.Connect(ctx, firstSchedulerAddress)
			if err != nil {
				return nil, err
			}
		}

		devStackNode := &DevStackNode{
			ComputeNode:   computeNode,
			RequesterNode: requesterNode,
			Transport:     transport,
			IpfsNode:      ipfsNode,
			IpfsClient:    ipfsClient,
			APIServer:     apiServer,
		}

		nodes = append(nodes, devStackNode)
	}

	return &DevStack{
		Nodes: nodes,
	}, nil
}

func (stack *DevStack) PrintNodeInfo() {
	if !config.DevstackGetShouldPrintInfo() {
		return
	}

	logString := ""

	for nodeIndex, node := range stack.Nodes {
		logString += fmt.Sprintf(`
export IPFS_PATH_%d=%s
export API_PORT_%d=%d`,
			nodeIndex,
			node.IpfsNode.RepoPath,
			nodeIndex,
			stack.Nodes[0].APIServer.Port,
		)
	}

	for nodeIndex, node := range stack.Nodes {
		logString += fmt.Sprintf(`
-------------------------------
node %d
-------------------------------

export IPFS_API_PORT_%d=%d
export IPFS_PATH_%d=%s
export API_PORT_%d=%d
cid=$(ipfs --api /ip4/127.0.0.1/tcp/${IPFS_API_PORT_%d} add --quiet ./testdata/grep_file.txt)
curl -XPOST http://127.0.0.1:%d/api/v0/id
`,
			nodeIndex,
			nodeIndex,
			node.IpfsNode.APIPort,
			nodeIndex,
			node.IpfsNode.RepoPath,
			nodeIndex,
			stack.Nodes[0].APIServer.Port,
			nodeIndex,
			node.IpfsNode.APIPort,
		)
	}

	log.Info().Msg(logString)
}

func (stack *DevStack) AddFileToNodes(nodeCount int, filePath string) (string, error) {
	var res string
	for i, node := range stack.Nodes {
		if i >= nodeCount {
			continue
		}

		cid, err := node.IpfsClient.Put(context.Background(), filePath)
		if err != nil {
			return "", fmt.Errorf("error adding file to node %d: %v", i, err)
		}

		log.Debug().Msgf("Added cid '%s' to ipfs node '%s'", cid, node.IpfsNode.ID())
		res = strings.TrimSpace(cid)
	}

	return res, nil
}

func (stack *DevStack) AddTextToNodes(nodeCount int, fileContent []byte) (string, error) {
	testDir, err := ioutil.TempDir("", "bacalhau-test")
	if err != nil {
		return "", err
	}

	testFilePath := fmt.Sprintf("%s/test.txt", testDir)
	err = os.WriteFile(testFilePath, fileContent, util.OS_USER_RW)
	if err != nil {
		return "", err
	}

	return stack.AddFileToNodes(nodeCount, testFilePath)
}

func (stack *DevStack) GetJobStates(ctx context.Context, jobID string) (map[string]executor.JobStateType, error) {
	apiClient := publicapi.NewAPIClient(stack.Nodes[0].APIServer.GetURI())

	job, ok, err := apiClient.Get(ctx, jobID)
	if err != nil {
		return nil, fmt.Errorf(
			"devstack: error fetching job %s: %v", jobID, err)
	}
	if !ok {
		return nil, nil
	}

	states := map[string]executor.JobStateType{}
	for id, state := range job.State {
		states[id] = state.State
	}

	return states, nil
}

// a function that is given a map of nodeid -> job states
// and will throw an error if anything about that is wrong
type CheckJobStatesFunction func(map[string]executor.JobStateType) (bool, error)

// there should be zero errors with any job
func WaitForJobThrowErrors(errorStates []executor.JobStateType) CheckJobStatesFunction {
	return func(jobStates map[string]executor.JobStateType) (bool, error) {
		log.Trace().Msgf("WaitForJobThrowErrors:\nerrorStates = %+v,\njobStates = %+v", errorStates, jobStates)
		for id, state := range jobStates {
			if system.StringArrayContains(system.GetJobStateStringArray(errorStates), state.String()) {
				return false, fmt.Errorf("job %s has error state: %s", id, state.String())
			}
		}
		return true, nil
	}
}

// there must be exactly len(nodeIds)
// each state must be the given type
// each seen node id must be present in the presented array
// this is useful for testing (did only the nodes that should have completed the job run it)
func WaitForJobAllHaveState(nodeIDs []string, states ...executor.JobStateType) CheckJobStatesFunction {
	return func(jobStates map[string]executor.JobStateType) (bool, error) {
		log.Trace().Msgf("WaitForJobShouldHaveStates:\nnodeIds = %+v,\nstate = %s\njobStates = %+v", nodeIDs, states, jobStates)
		if len(jobStates) != len(nodeIDs) {
			return false, nil
		}
		seenAll := true
		for _, nodeID := range nodeIDs {
			seenState, ok := jobStates[nodeID]
			if !ok {
				seenAll = false
			} else if !system.StringArrayContains(
				system.GetJobStateStringArray(states), seenState.String()) {
				seenAll = false
			}
		}
		return seenAll, nil
	}
}

// if there are > X states then error
func WaitDontExceedCount(count int) CheckJobStatesFunction {
	return func(jobStates map[string]executor.JobStateType) (bool, error) {
		if len(jobStates) > count {
			return false, fmt.Errorf("there are more states: %d than expected: %d", len(jobStates), count)
		}
		return true, nil
	}
}

func (stack *DevStack) WaitForJobWithLogs(
	ctx context.Context,
	jobID string,
	shouldLog bool,
	checkJobStateFunctions ...CheckJobStatesFunction,
) error {
	waiter := &system.FunctionWaiter{
		Name:        "wait for job",
		MaxAttempts: 100,
		Delay:       time.Second * 1,
		Handler: func() (bool, error) {
			// load the current states of the job
			states, err := stack.GetJobStates(ctx, jobID)
			if shouldLog {
				spew.Dump(states)
			}
			if err != nil {
				return false, err
			}

			allOk := true
			for _, checkFunction := range checkJobStateFunctions {
				stepOk, err := checkFunction(states)
				if err != nil {
					return false, err
				}
				if !stepOk {
					allOk = false
				}
			}

			// If all the jobs are in terminal states, then nothing is going
			// to change if we keep polling, so we should exit early.
			allTerminal := len(states) == len(stack.Nodes)
			for _, state := range states {
				if !state.IsTerminal() {
					allTerminal = false
					break
				}
			}
			if allTerminal && !allOk {
				return false, fmt.Errorf("all jobs are in terminal states and conditions aren't met")
			}

			return allOk, nil
		},
	}

	return waiter.Wait()
}

func (stack *DevStack) WaitForJob(
	ctx context.Context,
	jobID string,
	checkJobStateFunctions ...CheckJobStatesFunction,
) error {
	return stack.WaitForJobWithLogs(ctx, jobID, false, checkJobStateFunctions...)
}

func (stack *DevStack) GetNode(ctx context.Context, nodeID string) (
	*DevStackNode, error) {
	for _, node := range stack.Nodes {
		id, err := node.Transport.HostID(ctx)
		if err != nil {
			return nil, err
		}

		if id == nodeID {
			return node, nil
		}
	}

	return nil, fmt.Errorf("node not found: %s", nodeID)
}

func (stack *DevStack) GetNodeIds() ([]string, error) {
	ids := []string{}
	for _, node := range stack.Nodes {
		id, err := node.Transport.HostID(context.Background())
		if err != nil {
			return ids, err
		}
		ids = append(ids, id)
	}

	return ids, nil
}

func (stack *DevStack) GetShortIds() ([]string, error) {
	ids, err := stack.GetNodeIds()
	if err != nil {
		return ids, err
	}
	shortids := []string{}
	for _, id := range ids {
		shortids = append(shortids, system.ShortID(id))
	}
	return shortids, nil
}

func newSpan(name string) (context.Context, trace.Span) {
	return system.Span(context.Background(), "devstack", name)
}
