#!/bin/bash
set -e

pushd $(dirname $0) &>/dev/null
CURDIR=${PWD}
popd &>/dev/null

go clean -testcache
go mod vendor

export VERBOSE=-test.v
export SSH_KEYFILE=id_rsa
export TEST_MODE=0
export TEST_SERVER_CONFIG=${CURDIR}/../config/server.json
export TEST_PROVIDER_CONFIG=${CURDIR}/../config/vsphere/provider.json
export TEST_CONFIG=${CURDIR}/../config/vsphere/config.json
export TEST_MACHINES_CONFIG=${CURDIR}/../config/vsphere/machines.json
export PLATEFORM=vsphere
export DEFAULT_MACHINE=medium

source "${CURDIR}/providers.sh"
source "${CURDIR}/server.sh"
source "${CURDIR}/nodegroup.sh"
