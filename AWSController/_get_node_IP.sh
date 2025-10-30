#!/bin/bash
start=`date +%s.%N`
# Check is running
# instances.id 파일 여부 확인
DEPLOY_PATH="../common"
IPS_FILE_PATH="${DEPLOY_PATH}/ips.txt"
PUBLIC_IPS_FILE_PATH="${DEPLOY_PATH}/base_ips.txt"

if [ $1 == "y" ]; then
  python3 instance_id_parsing.py
fi

INSTANCES_FILE_FILE=instances.node.file

# shard0_nums=(12 13 14 18 12 11 13 15 12 8)
# shard1_nums=(14 17 13 11 14 14 20 17 14 8)
# shard2_nums=(14 20 13 11 14 15 17 18 14 8)
# shard0=()
# shard1=()
# shard2=()

order=0
# Get Instances File 
if [ ! -f "${INSTANCES_FILE_FILE}" ]; then
  printf "No Instance Files are existing.\n"
else

  # Delete Existed IP and Public DNS Files
  rm -rf $IPS_FILE_PATH 

  # Execute Command for each Instance File
  while read line
  do
    REGION=$(echo $line | cut -d " " -f 1)
    INSTANCE_ID=$(echo $line | cut -d " " -f 2)
    echo "REGION=${REGION}, INSTANCES_ID=${INSTANCE_ID}"

    # Get Status of Instances by IDs
    INSTANCES_STATE_array=(`aws ec2 describe-instance-status --instance-ids "$INSTANCE_ID" --region "$REGION" --query "InstanceStatuses[].InstanceState.Name"`)
    length=`expr ${#INSTANCES_STATE_array[*]} - 2`
    printf "$length instances exists..\n"
    
    # Check Instances status is running
    printf "==========================================\n"
    printf "Now Check If They are running\n"
    i=1
    while [ $i -le $length ]
    do
      # echo $i
      
      status=`echo ${INSTANCES_STATE_array[$i]} | cut -d '"' -f 2`
      ## IF Running
      echo $status
      if [ $status == "running" ]; then
        echo "${i}th Instance is ready, ID: ${INSTANCES_ID_array[$i]}"
        ((i++))
      else ## IF Not Running Wait for 1 second
        sleep 1s
      fi
    done
    
    # If All Nodes are running state, Get Public DNS
    printf "==========================================\n"
    printf "All Nodes are running.. now get public DNS\n"
    PUBLIC_IP=$(aws ec2 describe-instances --instance-ids "$INSTANCE_ID" --region "$REGION" --query "Reservations[].Instances[].PublicIpAddress")
    PUBLIC_IP=$(echo $PUBLIC_IP | cut -d " " -f 2)
    PUBLIC_IP=$(echo $PUBLIC_IP | cut -d '"' -f 2)
    echo ${PUBLIC_IP} >> $IPS_FILE_PATH
  done < $INSTANCES_FILE_FILE

  printf "Get ${INSTANCES_FILE_FILE} Completed!..\n"
  printf "Lastly Add these instances' Public IP in known_hosts files \n"
  rm -rf ~/.ssh/known_hosts
  # ssh-keyscan -t ed25519 -f "$IPS_FILE_PATH" >> ~/.ssh/known_hosts
  printf "All Completed!..\n"
fi



# finish=`date +%s.%N`
# diff=$( echo "$finish - $start" | bc -l )
# echo 'start:' $start
# echo 'finish:' $finish
# echo 'diff:' $diff
