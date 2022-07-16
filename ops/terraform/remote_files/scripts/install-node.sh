#!/bin/bash
set -euo pipefail
IFS=$'\n\t'

source /terraform_node/variables

function install-docker() {
  sudo apt-get install -y \
      ca-certificates \
      curl \
      gnupg \
      lsb-release
  sudo mkdir -p /etc/apt/keyrings
  curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo gpg --dearmor -o /etc/apt/keyrings/docker.gpg
  echo \
    "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu \
    $(lsb_release -cs) stable" | sudo tee /etc/apt/sources.list.d/docker.list > /dev/null
  sudo apt-get update -y
  sudo apt-get install -y docker-ce docker-ce-cli containerd.io docker-compose-plugin
}

# Lay down a very basic web server to report when the node is healthy
function install-healthcheck() {
  sudo apt-get -y install --no-install-recommends wget gnupg ca-certificates
  wget -O - https://openresty.org/package/pubkey.gpg | sudo apt-key add -
  echo "deb http://openresty.org/package/ubuntu $(lsb_release -sc) main" \
      | sudo tee /etc/apt/sources.list.d/openresty.list
  sudo apt-get update -y
  sudo apt-get -y install --no-install-recommends openresty
  sudo cp /terraform_node/nginx.conf /usr/local/openresty/nginx/conf/nginx.conf
}

function install-ipfs() {
  wget "https://dist.ipfs.io/go-ipfs/${IPFS_VERSION}/go-ipfs_${IPFS_VERSION}_linux-amd64.tar.gz"
  tar -xvzf "go-ipfs_${IPFS_VERSION}_linux-amd64.tar.gz"
  cd go-ipfs
  sudo bash install.sh
  ipfs --version
}

function install-bacalhau() {
  wget "https://github.com/filecoin-project/bacalhau/releases/download/${BACALHAU_VERSION}/bacalhau_${BACALHAU_VERSION}_linux_amd64.tar.gz"
  tar xfv "bacalhau_${BACALHAU_VERSION}_linux_amd64.tar.gz"
  sudo mv ./bacalhau /usr/local/bin/bacalhau
}

function mount-disk() { 
  # wait for /dev/sdb to exist
  while [ ! -e /dev/sdb ]; do
    sleep 1
    echo "waiting for /dev/sdb to exist"
  done
  # mount /dev/sdb at /data
  sudo mkdir -p /data
  sudo mount /dev/sdb /data || (sudo mkfs -t ext4 /dev/sdb && sudo mount /dev/sdb /data) 
}

# make sure that "ipfs init" has been run
function init-ipfs() {
  sudo mkdir -p /data/ipfs
  export IPFS_PATH=/data/ipfs

  if [ ! -e /data/ipfs/version ]; then
    ipfs init
  fi
}

# install any secrets provided as terraform vars
function install-secrets() {
  # set defaults
  export HONEYCOMB_KEY=""
  if [ -e /data/secrets.sh ]; then
    source /data/secrets.sh
  fi

  # load new values if they were provided
  if [ ! -z "${SECRETS_HONEYCOMB_KEY}" ]; then
    export HONEYCOMB_KEY="${SECRETS_HONEYCOMB_KEY}"
  fi

  # write the secrets to persistent disk
  sudo tee /data/secrets.sh > /dev/null <<EOG
export HONEYCOMB_KEY="${HONEYCOMB_KEY}"
EOG
}

# if we are node zero, are in unsafe mode and don't have a private key
# then let's copy the unsafe private key so we have a deterministic id
# that other nodes will connect to
function init-bacalhau() {
  export BACALHAU_NODE_PRIVATE_KEY_PATH="/data/.bacalhau/private_key.${BACALHAU_PORT}"
  sudo mkdir -p /data/.bacalhau
  if [ "$TERRAFORM_NODE_INDEX" == "0" ] && [ -n "$BACALHAU_UNSAFE_CLUSTER" ] && [ ! -f "$BACALHAU_NODE_PRIVATE_KEY_PATH" ]; then
    echo "WE ARE NOW INSTALLING THE UNSAFE KEY YO"
    sudo cp /terraform_node/bacalhau-unsafe-private-key "$BACALHAU_NODE_PRIVATE_KEY_PATH"
    sudo chmod 0600 "$BACALHAU_NODE_PRIVATE_KEY_PATH"
  fi
}

function start-services() {
  sudo systemctl daemon-reload
  sudo systemctl enable ipfs-daemon.service
  sudo systemctl enable bacalhau-daemon.service
  sudo systemctl start ipfs-daemon
  sudo systemctl start bacalhau-daemon
  sudo service openresty reload
}

function install() {
  install-docker
  install-healthcheck
  install-ipfs
  install-bacalhau
  mount-disk
  init-ipfs
  init-bacalhau
  install-secrets
  start-services
}

install
