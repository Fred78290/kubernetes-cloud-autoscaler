#!/bin/bash
pushd $(dirname $0) &>/dev/null
CURDIR=${PWD}
popd &>/dev/null

nohup ${HOME}/go/bin/vcsim -pg 2 &

GOVC_SIM_PID=$!

echo "Launched vcsim ${GOVC_SIM_PID}"

mkdir -p ${CURDIR}/../../.config

echo -n "${GOVC_SIM_PID}" > ${CURDIR}/../../.config/vcsim.pid

disown ${GOVC_SIM_PID}

ps ax | grep vcsim
