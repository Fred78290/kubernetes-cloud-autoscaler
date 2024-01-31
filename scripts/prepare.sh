#!/bin/bash
SRCDIR=$(dirname $0)
PLATEFORM=$1
NODEGROUP=$2
CONFIG_DIR=${SRCDIR}/../.config/${NODEGROUP}

rm -rf /tmp/autoscaler.sock

if [ -d ${HOME}/Projects/GitHub/autoscaled-masterkube-multipass ]; then
	AUTOSCALER_HOME=${HOME}/Projects/GitHub/autoscaled-masterkube-multipass
elif [ -d ${HOME}/Projects/autoscaled-masterkube-multipass ]; then
	AUTOSCALER_HOME=${HOME}/Projects/autoscaled-masterkube-multipass
else
	echo "autoscaled-masterkube-multipass not found"
	exit 1
fi

if [ -n "${NODEGROUP}" ]; then
	mkdir -p "${CONFIG_DIR}"

	cp ${AUTOSCALER_HOME}/cluster/${NODEGROUP}/config ${CONFIG_DIR}/config
	cp ${AUTOSCALER_HOME}/config/${NODEGROUP}/config/provider.json ${CONFIG_DIR}/provider.json
	cp ${AUTOSCALER_HOME}/config/${NODEGROUP}/config/machines.json ${CONFIG_DIR}/machines.json

	cat ${AUTOSCALER_HOME}/config/${NODEGROUP}/config/autoscaler.json | jq \
		--arg ETCD_SSL_DIR "${AUTOSCALER_HOME}/cluster/${NODEGROUP}/etcd" \
		--arg PKI_DIR "${AUTOSCALER_HOME}/cluster/${NODEGROUP}/kubernetes/pki" \
		--arg SSH_KEY "${HOME}/.ssh/id_rsa" \
		'. | .listen = "unix:/tmp/autoscaler.sock" | ."src-etcd-ssl-dir" = $ETCD_SSL_DIR | ."kubernetes-pki-srcdir" = $PKI_DIR | ."ssh-infos"."ssh-private-key" = $SSH_KEY' > ${CONFIG_DIR}/autoscaler.json
fi
