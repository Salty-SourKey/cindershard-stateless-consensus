#!/bin/bash

DEPLOY_PATH="../common"
IPS_FILE_PATH="${DEPLOY_PATH}/ips.txt"
PUBLIC_IPS_FILE_PATH="${DEPLOY_PATH}/base_ips.txt"

INSTANCES_BUILDER_FILE=instances.builder.file

# instances.file 파일 여부 확인
if [ ! -f "${INSTANCES_BUILDER_FILE}" ]; then
  printf "No Instance Files are existing.\n"
else
  while read line
  do
    REGION=$(echo $line | cut -d " " -f 1)
    INSTANCE_ID=$(echo $line | cut -d " " -f 2)
    echo "REGION=${REGION}, INSTANCE_ID=${INSTANCE_ID}"

    aws ec2 terminate-instances --instance-ids "$INSTANCE_ID" --region "$REGION" > /dev/null
  done < $INSTANCES_BUILDER_FILE
fi

INSTANCES_NODE_FILE=instances.node.file

# instances.file 파일 여부 확인
if [ ! -f "${INSTANCES_NODE_FILE}" ]; then
  printf "No Instance Files are existing.\n"
else
  while read line
  do
    REGION=$(echo $line | cut -d " " -f 1)
    INSTANCE_ID=$(echo $line | cut -d " " -f 2)
    echo "REGION=${REGION}, INSTANCE_ID=${INSTANCE_ID}"

    aws ec2 terminate-instances --instance-ids "$INSTANCE_ID" --region "$REGION" > /dev/null
  done < $INSTANCES_NODE_FILE
fi

rm -rf ${INSTANCES_BUILDER_FILE} ${INSTANCES_NODE_FILE}
rm -rf "${IPS_FILE_PATH}" "${PUBLIC_IPS_FILE_PATH}" 