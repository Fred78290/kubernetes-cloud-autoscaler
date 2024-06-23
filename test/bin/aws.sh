#!/bin/bash
set -e

pushd $(dirname $0) &>/dev/null
CURDIR=${PWD}
popd &>/dev/null

go clean -testcache
go mod vendor

export LOCALDEFS=${CURDIR}/../local.env
export VERBOSE=-test.v
export TEST_MODE=0
export TEST_SERVER_CONFIG=${CURDIR}/../config/server.json
export TEST_PROVIDER_CONFIG=${CURDIR}/../config/aws/provider.json
export TEST_CONFIG=${CURDIR}/../config/aws/config.json
export TEST_MACHINES_CONFIG=${CURDIR}/../config/aws/machines.json
export PLATEFORM=aws
export DEFAULT_MACHINE=t4g.small

if [ ! -f "${LOCALDEFS}" ]; then
  echo "File ${LOCALDEFS} not found, exit test"
  exit 1
fi

source ${LOCALDEFS}

if [ -z "${AWS_ACCESSKEY}" ] && [ -z "${AWS_PROFILE}" ]; then
    echo "Neither AWS_ACCESSKEY or AWS_PROFILE are defined, exit test"
    exit 1
fi

if [ -n "${SSH_PRIVATEKEY}" ] && [ ! -f ${HOME}/.ssh/${SSH_KEYFILE} ]; then
  mkdir -p ${HOME}/.ssh

  echo -n ${SSH_PRIVATEKEY} | base64 -d > ${HOME}/.ssh/${SSH_KEYFILE}

  chmod 0600 ${HOME}/.ssh/${SSH_KEYFILE}
fi

source "${CURDIR}/providers.sh"
source "${CURDIR}/server.sh"
source "${CURDIR}/nodegroup.sh"
