#!/bin/bash
CURDIR=$(dirname $0)

if [ -z "${GITHUB_RUN_ID}" ]; then
    echo "Can't run out of github action"
    exit 1
fi

cat > ${CURDIR}/../local.env <<EOF
export SSH_KEYFILE=test_rsa
export SSH_PRIVATEKEY=$SSH_PRIVATEKEY
export SEED_IMAGE=$SEED_IMAGE
export SEED_USER=$SEED_USER
export KUBE_ENGINE=external

export AWS_IAM_ROLE_ARN=$IAM_ROLE_ARN
export AWS_SSH_KEYNAME=$SSH_KEYNAME
export AWS_SEED_IMAGE=$SEED_IMAGE

export AWS_VPC_SECURITY_GROUPID=$VPC_SECURITY_GROUPID
export AWS_VPC_SUBNET_ID=$VPC_SUBNET_ID
export AWS_ROUTE53_ZONEID=$ROUTE53_ZONEID

export AWS_PROFILE=$AWS_PROFILE
export AWS_REGION=$AWS_REGION
export AWS_ACCESSKEY=$AWS_ACCESSKEY
export AWS_SECRETKEY=$AWS_SECRETKEY

export PRIVATE_DOMAIN_NAME=$PRIVATE_DOMAIN_NAME
export PUBLIC_DOMAIN_NAME=$PUBLIC_DOMAIN_NAME
export DOMAIN_RESOLVER=8.8.8.8

export GOVC_URL=https://127.0.0.1:8989/sdk
export GOVC_USERNAME=user
export GOVC_PASSWORD=pass
export GOVC_DATACENTER=DC0
export GOVC_DATASTORE=LocalDS_0
export GOVC_RESOURCE_POOL=/DC0/host/DC0_H0/Resources
export GOVC_FOLDER=
export GOVC_TEMPLATE_NAME=DC0_H0_VM0

export GOVC_NETWORK_PRIVATE="DVS0"
export GOVC_NETWORK_PUBLIC="VM Network"
EOF

export | grep GITHUB | sed 's/declare -x/export/g' >> ${CURDIR}/local.env

make -e REGISTRY=fred78290 -e TAG=test-ci test-in-docker
