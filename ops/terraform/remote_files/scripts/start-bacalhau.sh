#!/bin/bash
set -euo pipefail
IFS=$'\n\t'

# import the terraform vars
source /terraform_node/variables

# import the secrets
source /data/secrets.sh

# pick between the configured nodeid and the unsafe one
export USE_NODE0_ID="$BACALHAU_CONNECT_NODE0"

# if we don't have a node0 id and are in unsafe mode so can use the unsafe id
if [ -z "$BACALHAU_CONNECT_NODE0" ] && [ -n "$BACALHAU_UNSAFE_CLUSTER" ]; then
  export USE_NODE0_ID="$BACALHAU_NODE0_UNSAFE_ID"
fi

# the fully exploded multiaddress for node0
export NODE0_MULTIADDRESS="/ip4/$TERRAFORM_NODE0_IP/tcp/$BACALHAU_PORT/p2p/$USE_NODE0_ID"

# work out if we actually want to connect to that multiaddress
export CONNECT_PEER="none"

# if we are > node0 and have either an explicit node0 id or are in unsafe mode - then we do want to connect
if [ "$TERRAFORM_NODE_INDEX" != "0" ] && [ -n "$USE_NODE0_ID" ]; then
  export CONNECT_PEER="$NODE0_MULTIADDRESS"
fi

bacalhau serve \
  --job-selection-data-locality anywhere \
  --ipfs-connect /ip4/127.0.0.1/tcp/5001 \
  --port $BACALHAU_PORT \
  --peer $CONNECT_PEER
