USERNAME=$1
SERVER_ADDR=$2

# scp ../start_protocol.sh ${USERNAME}@${SERVER_ADDR}:/home/${USERNAME}/bin
scp ../server ${USERNAME}@${SERVER_ADDR}:/home/${USERNAME}/bin
# scp ./config.conf ${USERNAME}@${SERVER_ADDR}:/home/${USERNAME}/bin