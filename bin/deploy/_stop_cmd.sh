USERNAME=$1
SERVER_ADDR=$2

echo -e "Node Stop"
# ssh ${USERNAME}@${SERVER_ADDR} "cd bin; ./server -sim=false -mode=gateway -shard=0 -id=0 2> error.log &"
ssh ${USERNAME}@${SERVER_ADDR} "cd bin; ./stop_protocol.sh"
