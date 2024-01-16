#!/bin/bash
SRCDIR=$(dirname $0)
AGENT=$1
CONFIG=$2
CONFIG_DIR=${SRCDIR}/../.config/${AGENT}/${CONFIG}

rm -rf /tmp/autoscaler.sock

AUTOSCALER_HOME=${HOME}/Projects/GitHub/autoscaled-masterkube-${AGENT}

if [ -n "${CONFIG}" ]; then
	mkdir -p "${CONFIG_DIR}"

	cp ${AUTOSCALER_HOME}/cluster/${CONFIG}/config ${CONFIG_DIR}/config
	cp ${AUTOSCALER_HOME}/config/${CONFIG}/config/provider.json ${CONFIG_DIR}/provider.json
	cp ${AUTOSCALER_HOME}/config/${CONFIG}/config/machines.json ${CONFIG_DIR}/machines.json

	cat ${AUTOSCALER_HOME}/config/${CONFIG}/config/autoscaler.json | jq \
		--arg ETCD_SSL_DIR "${AUTOSCALER_HOME}/cluster/${CONFIG}/etcd" \
		--arg PKI_DIR "${AUTOSCALER_HOME}/cluster/${CONFIG}/kubernetes/pki" \
		--arg SSH_KEY "${HOME}/.ssh/id_rsa" \
		'. | .listen = "unix:/tmp/autoscaler.sock" | ."src-etcd-ssl-dir" = $ETCD_SSL_DIR | ."kubernetes-pki-srcdir" = $PKI_DIR | ."ssh-infos"."ssh-private-key" = $SSH_KEY' > ${CONFIG_DIR}/autoscaler.json
fi
