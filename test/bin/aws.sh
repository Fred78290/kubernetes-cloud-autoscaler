#!/bin/bash
set -e

pushd $(dirname $0)
CURDIR=${PWD}
popd

LOCALDEFS=${CURDIR}/../local.env
VERBOSE=-test.v

if [ ! -f "${LOCALDEFS}" ]; then
  echo "File ${LOCALDEFS} not found, exit test"
  exit 1
fi

source ${LOCALDEFS}

if [ -z "${AWS_ACCESSKEY}" ] && [ -z "${AWS_PROFILE}" ]; then
    echo "Neither AWS_ACCESSKEY or AWS_PROFILE are defined, exit test"
    exit 1
fi

mkdir -p ${HOME}/.ssh

echo -n ${SSH_PRIVATEKEY} | base64 -d > ${HOME}/.ssh/test_rsa

export Test_AuthMethodKey=NO
export Test_Sudo=NO
export Test_getInstanceID=YES
export Test_createInstance=YES
export Test_statusInstance=YES
export Test_waitForPowered=YES
export Test_waitForIP=YES
export Test_powerOnInstance=NO
export Test_powerOffInstance=YES
export Test_shutdownInstance=YES
export Test_deleteInstance=YES

export TEST_AWS_CONFIG=${CURDIR}/../config/aws/provider.json
export TEST_CONFIG=${CURDIR}/../config/aws/config.json
export TEST_MACHINES_CONFIG=${CURDIR}/../config/aws/machines.json

go clean -testcache
go mod vendor

# Run this test only on github action
if [ -n "${GITHUB_REF}" ]; then
  echo "Run create instance"
  go test $VERBOSE --run Test_createInstance -timeout 60s -count 1 -race ./aws

  echo "Run get instance"
  go test $VERBOSE --run Test_getInstanceID -timeout 60s -count 1 -race ./aws

  echo "Run status instance"
  go test $VERBOSE --run Test_statusInstance -timeout 60s -count 1 -race ./aws

  echo "Run wait for started"
  go test $VERBOSE --run Test_waitForPowered -timeout 60s -count 1 -race ./aws

  echo "Run wait for IP"
  go test $VERBOSE --run Test_waitForIP -timeout 120s -count 1 -race ./aws

  #echo "Run power instance"
  #go test $VERBOSE --run Test_powerOffInstance -count 1 -race ./aws

  echo "Run shutdown instance"
  go test $VERBOSE --run Test_shutdownInstance -timeout 600s -count 1 -race ./aws

  echo "Run test delete instance"
  go test $VERBOSE --run Test_deleteInstance -timeout 60s -count 1 -race ./aws
fi

export TEST_CONFIG=${CURDIR}/../config/autscaler.json

echo "Run server test"

export TestServer=YES
export TestServer_NodeGroups=YES
export TestServer_NodeGroupForNode=YES
export TestServer_HasInstance=YES
export TestServer_Pricing=YES
export TestServer_GetAvailableMachineTypes=YES
export TestServer_NewNodeGroup=YES
export TestServer_GetResourceLimiter=YES
export TestServer_Cleanup=YES
export TestServer_Refresh=YES
export TestServer_TargetSize=YES
export TestServer_IncreaseSize=YES
export TestServer_DecreaseTargetSize=YES
export TestServer_DeleteNodes=YES
export TestServer_Id=YES
export TestServer_Debug=YES
export TestServer_Nodes=YES
export TestServer_TemplateNodeInfo=YES
export TestServer_Exist=YES
export TestServer_Create=YES
export TestServer_Delete=YES
export TestServer_Autoprovisioned=YES
export TestServer_Belongs=YES
export TestServer_NodePrice=YES
export TestServer_PodPrice=YES

go test $VERBOSE --test.short -timeout 1200s -race ./server -run Test_Server

echo "Run nodegroup test"

export TestNodegroup=YES
export TestNodeGroup_launchVM=YES
export TestNodeGroup_stopVM=YES
export TestNodeGroup_startVM=YES
export TestNodeGroup_statusVM=YES
export TestNodeGroup_deleteVM=YES
export TestNodeGroupGroup_addNode=YES
export TestNodeGroupGroup_deleteNode=YES
export TestNodeGroupGroup_deleteNodeGroup=YES

go test $VERBOSE --test.short -timeout 1200s -race ./server -run Test_Nodegroup
