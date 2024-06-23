#!/bin/bash
pushd $(dirname $0) &>/dev/null
CURDIR=${PWD}
popd &>/dev/null

ps ax | grep vcsim

if [ -f ${CURDIR}/../../.config/vcsim.pid ]; then
  GOVC_SIM_PID=$(cat ${CURDIR}/../../.config/vcsim.pid)

  echo "Kill vcsim ${GOVC_SIM_PID}"

  kill $(cat ${CURDIR}/../../.config/vcsim.pid)

  rm ${CURDIR}/../../.config/vcsim.pid
fi
