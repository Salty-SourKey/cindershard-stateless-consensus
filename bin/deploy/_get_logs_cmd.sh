USERNAME=$1
SERVER_ADDR=$2
SHARD=$3
INDEX=$4

scp ${USERNAME}@${SERVER_ADDR}:/home/${USERNAME}/bin/*.log ../../log/${SHARD}/${INDEX}
# ssh ${USERNAME}@${SERVER_ADDR} "cd bin; rm *.log"
