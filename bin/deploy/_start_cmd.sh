USERNAME=$1
SERVER_ADDR=$2
ISNODE=$3
SHARD=$4
INDEX=$5

VARIABLE=config.conf

K=1
while read line
do
    if [ ${K} -eq 1 ]; then
        CONFSHARD=$(echo $line | cut -d ":" -f 2)
        CONFSHARD=$(echo $CONFSHARD | cut -d " " -f 2)
    fi
    K=$((K+1))
done < $VARIABLE
CONFSHARD=$((CONFSHARD+1))

if [ ${ISNODE} -eq 0 ]; then
    if [ ${SHARD} -eq ${CONFSHARD} ]; then
        echo "Gateway Start"
        echo "shard: ${SHARD} id: ${INDEX}"
        # ssh ${USERNAME}@${SERVER_ADDR} "cd bin; ./server -sim=false -mode=gateway -shard=0 -id=0 2> error.log &"
        ssh ${USERNAME}@${SERVER_ADDR} "cd bin; ./start_protocol.sh ${SHARD} ${INDEX}"
    else
        echo "BlockBuilder ${SHARD} Start"
        echo "shard: ${SHARD} id: ${INDEX}"
        # ssh ${USERNAME}@${SERVER_ADDR} "cd bin; ./server -sim=false -mode=blockbuilder -shard=${SHARD} -id=${INDEX} 2> error.log &"
        ssh ${USERNAME}@${SERVER_ADDR} "cd bin; ./start_protocol.sh ${SHARD} ${INDEX}"
    fi
else
    echo "Node ${SHARD} ${INDEX} Start"
    echo "shard ${SHARD}, id ${INDEX}"
    # ssh ${USERNAME}@${SERVER_ADDR} "cd bin; ./server -sim=false -mode=blockbuilder -shard=${SHARD} -id=${INDEX} 2> error.log &"
    ssh ${USERNAME}@${SERVER_ADDR} "cd bin; ./start_protocol.sh ${SHARD} ${INDEX}"
fi
