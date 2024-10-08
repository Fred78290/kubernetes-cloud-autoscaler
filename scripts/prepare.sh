#!/bin/bash
SRCDIR=$(dirname $0)
PLATEFORM=$1
NODEGROUP=$2
CONFIG_DIR=${SRCDIR}/../.config/${PLATEFORM}/${NODEGROUP}

rm -rf /tmp/autoscaler.sock

if [ -d ${HOME}/Projects/GitHub/autoscaled-masterkube-multipass ]; then
	AUTOSCALER_HOME=${HOME}/Projects/GitHub/autoscaled-masterkube-multipass
elif [ -d ${HOME}/Projects/autoscaled-masterkube-multipass ]; then
	AUTOSCALER_HOME=${HOME}/Projects/autoscaled-masterkube-multipass
else
	echo "autoscaled-masterkube-multipass not found"
	exit 1
fi

AUTOSCALER_DESKTOP_UTILITY_TLS=$(dirname $(kubernetes-desktop-autoscaler-utility certificate generate | jq -r .ClientKey) | sed -e 's/\//\\\//g')
LXD_CONFIG=$(echo "${HOME}\/snap/lxd/common/config" | sed -e 's/\//\\\//g')
BIND9_RNDCKEY="${AUTOSCALER_HOME}/config/${NODEGROUP}/cluster/rndc.key"

if [ -n "${NODEGROUP}" ]; then
	mkdir -p "${CONFIG_DIR}"

	if [ -f ${AUTOSCALER_HOME}/config/${NODEGROUP}/config/grpc-config.json ]; then
		cat ${AUTOSCALER_HOME}/config/${NODEGROUP}/config/grpc-config.json | js '. | .address: "unix:/tmp/autoscaler.sock"' > ${CONFIG_DIR}/grpc-config.json
	else
		echo 'address: "unix:/tmp/autoscaler.sock"' > ${CONFIG_DIR}/grpc-config.yaml
	fi

	cp ${AUTOSCALER_HOME}/config/${NODEGROUP}/cluster/config ${CONFIG_DIR}/config
	cp ${AUTOSCALER_HOME}/config/${NODEGROUP}/config/machines.json ${CONFIG_DIR}/machines.json

	cat ${AUTOSCALER_HOME}/config/${NODEGROUP}/config/provider.json \
		| sed -e "s/\/etc\/ssl\/certs\/autoscaler-utility/${AUTOSCALER_DESKTOP_UTILITY_TLS}/g" \
		| jq --arg BIND9_RNDCKEY "${BIND9_RNDCKEY}" '.|."rndc-key-file" = $BIND9_RNDCKEY | ."lxd-server-url" = "unix:"' > ${CONFIG_DIR}/provider.json

	#cat ${AUTOSCALER_HOME}/config/${NODEGROUP}/config/provider.json \
	#	| sed -e "s/\/etc\/ssl\/certs\/autoscaler-utility/${AUTOSCALER_DESKTOP_UTILITY_TLS}/g" \
	#	| jq --arg BIND9_RNDCKEY "${BIND9_RNDCKEY}" '.|."rndc-key-file" = $BIND9_RNDCKEY 
	#	| ."lxd-config-location" = "/home/stack/snap/lxd/common/config" 
	#	| ."tls-server-cert" = "servercerts/stack.crt"
	#	| ."tls-client-cert" = "client.crt"
	#	| ."tls-client-key" = "client.key"
	#	| ."lxd-server-url" = "https://10.0.0.21:8443"' > ${CONFIG_DIR}/provider.json

	cat ${AUTOSCALER_HOME}/config/${NODEGROUP}/config/autoscaler.json | jq \
		--arg ETCD_SSL_DIR "${AUTOSCALER_HOME}/config/${NODEGROUP}/cluster/etcd" \
		--arg PKI_DIR "${AUTOSCALER_HOME}/config/${NODEGROUP}/cluster/kubernetes/pki" \
		--arg SSH_KEY "${HOME}/.ssh/id_rsa" \
		'. | .listen = "unix:/tmp/autoscaler.sock" | ."src-etcd-ssl-dir" = $ETCD_SSL_DIR | ."kubernetes-pki-srcdir" = $PKI_DIR | ."ssh-infos"."ssh-private-key" = $SSH_KEY' > ${CONFIG_DIR}/autoscaler.json
fi
