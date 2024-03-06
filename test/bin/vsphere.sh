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

mkdir -p ${HOME}/.ssh

echo -n ${SSH_PRIVATEKEY} | base64 -d > ${HOME}/.ssh/test_rsa

export Test_AuthMethodKey=NO
export Test_Sudo=NO
export Test_CIDR=YES
export Test_getVM=YES
export Test_listVM=YES
export Test_createVM=YES
export Test_statusVM=YES
export Test_powerOnVM=YES
export Test_powerOffVM=YES
export Test_shutdownGuest=YES
export Test_deleteVM=YES

export TEST_VSPHERE_CONFIG=${CURDIR}/../config/vsphere/provider.json
export TEST_CONFIG=${CURDIR}/../config/vsphere/config.json
export TEST_MACHINES_CONFIG=${CURDIR}/../config/vsphere/machines.json

go clean -testcache
go mod vendor

function cleanup {
  echo "Kill vcsim"
  kill $GOVC_SIM_PID
}

trap cleanup EXIT

echo "Launch vcsim"
vcsim -pg 2 &
GOVC_SIM_PID=$!

echo "Run vsphere test"
go test --test.short $VERBOSE -race ./providers/vsphere

kill $GOVC_SIM_PID &> /dev/null

vcsim -pg 2 &
GOVC_SIM_PID=$!

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

go test --test.short $VERBOSE -race ./server -run Test_Server

kill $GOVC_SIM_PID &> /dev/null

vcsim -pg 2 &
GOVC_SIM_PID=$!

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

go test --test.short $VERBOSE -race ./server -run Test_Nodegroup

kill $GOVC_SIM_PID &> /dev/null
