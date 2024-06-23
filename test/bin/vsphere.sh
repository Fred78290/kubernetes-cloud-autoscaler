#!/bin/bash
set -e

pushd $(dirname $0) &>/dev/null
CURDIR=${PWD}
popd &>/dev/null

go clean -testcache
go mod vendor

export LOCALDEFS=${CURDIR}/../local.env
export VERBOSE=-test.v
export TEST_MODE=1
export TEST_SERVER_CONFIG=${CURDIR}/../config/server.json
export TEST_PROVIDER_CONFIG=${CURDIR}/../config/vsphere/provider.json
export TEST_CONFIG=${CURDIR}/../config/vsphere/config.json
export TEST_MACHINES_CONFIG=${CURDIR}/../config/vsphere/machines.json
export PLATEFORM=vsphere
export DEFAULT_MACHINE=medium

if [ ! -f "${LOCALDEFS}" ]; then
  echo "File ${LOCALDEFS} not found, exit test"
  exit 1
fi

source ${LOCALDEFS}

if [ -n "${SSH_PRIVATEKEY}" ] && [ ! -f ${HOME}/.ssh/${SSH_KEYFILE} ]; then
  mkdir -p ${HOME}/.ssh

  echo -n ${SSH_PRIVATEKEY} | base64 -d > ${HOME}/.ssh/${SSH_KEYFILE}

  chmod 0600 ${HOME}/.ssh/${SSH_KEYFILE}
fi

function cleanup {
  echo "Kill vcsim"
  kill $GOVC_SIM_PID
}

trap cleanup EXIT

echo "Launch vcsim"
vcsim -pg 2 &
GOVC_SIM_PID=$!

source "${CURDIR}/providers.sh"

kill $GOVC_SIM_PID &> /dev/null

vcsim -pg 2 &
GOVC_SIM_PID=$!

source "${CURDIR}/server.sh"

kill $GOVC_SIM_PID &> /dev/null

vcsim -pg 2 &
GOVC_SIM_PID=$!

source "${CURDIR}/nodegroup.sh"

kill $GOVC_SIM_PID &> /dev/null
