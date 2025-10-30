USERNAME=$1
SERVER_ADDR=$2
INDEX=$3

ssh ${USERNAME}@${SERVER_ADDR} mkdir bin
ssh ${USERNAME}@${SERVER_ADDR} mkdir common
echo "upload replica ${INDEX}: ${USERNAME}@${SERVER_ADDR}"
scp ../server ${USERNAME}@${SERVER_ADDR}:/home/${USERNAME}/bin
scp ../client ${USERNAME}@${SERVER_ADDR}:/home/${USERNAME}/bin
scp ../start_protocol.sh ${USERNAME}@${SERVER_ADDR}:/home/${USERNAME}/bin
scp ../stop_protocol.sh ${USERNAME}@${SERVER_ADDR}:/home/${USERNAME}/bin
scp ./config.conf ${USERNAME}@${SERVER_ADDR}:/home/${USERNAME}/bin
scp -r ../../common ${USERNAME}@${SERVER_ADDR}:/home/${USERNAME}