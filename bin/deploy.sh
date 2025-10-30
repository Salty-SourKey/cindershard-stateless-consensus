SERVER_PID_FILE=server.pid
SERVER_PID=$(cat "${SERVER_PID_FILE}");

VARIABLE=./deploy/config.conf

K=1
while read line
do
    if [ ${K} -eq 1 ]; then
        SHARD=$(echo $line | cut -d ":" -f 2)
        SHARD=$(echo $SHARD | cut -d " " -f 2)
    elif [ ${K} -eq 2 ]; then
        COMMITTEE=$(echo $line | cut -d ":" -f 2)
        COMMITTEE=$(echo $COMMITTEE | cut -d " " -f 2)
    elif [ ${K} -eq 3 ]; then
        VALIDATOR=$(echo $line | cut -d ":" -f 2)
        VALIDATOR=$(echo $VALIDATOR | cut -d " " -f 2)
    fi
    K=$((K+1))
done < $VARIABLE
echo "Shard: ${SHARD}, Committee: ${COMMITTEE} Validator: ${VALIDATOR}"
SHARD=$((SHARD+1))
TOTALCOMMITTEE=$((SHARD*COMMITTEE))
SHARDPERNODE=$((COMMITTEE+VALIDATOR))

if [ -z "${SERVER_PID}" ]; then
    echo "Process id for servers is written to location: {$SERVER_PID_FILE}"
    rm *.log
    rm *.csv
    go build ../main/
    go build ../client/
    ./main -sim=false -mode=gateway -shard=0 -id=0 2> errorGate.log &
    echo $! >> ${SERVER_PID_FILE}
    sleep 0.5
    for (( i=1; i<${SHARD}; i=i+1 ))
    do
        for (( j=1; j<=${SHARDPERNODE}; j=j+1 ))
            do
                ./main -sim=false -mode=node -shard=$i -id=$j 2> errorShard${i}Node$j.log &
                echo $! >> ${SERVER_PID_FILE}
                sleep 0.1
            done
    done
    echo "${alg} is running, on log severity: ${severity}"
    echo "=== if error occurs, plz check error.log ==="
else
    echo "Servers are already started in this folder."
    exit 0
fi