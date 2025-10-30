SHARD=$1
ID=$2

VARIABLE=./config.conf

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
echo "confshard: ${CONFSHARD}"

SERVER_PID_FILE=server.pid
SERVER_PID=$(cat "${SERVER_PID_FILE}");
if [ -z "${SERVER_PID}" ]; then
    echo "Process id for servers is written to location: {$SERVER_PID_FILE}"
    rm *.log
    rm *.csv
    if [ ${SHARD} -eq ${CONFSHARD} ]; then
        ./server -sim=false -mode=gateway -shard=${SHARD} -id=${ID} 2> error.log &
    else
        if [ ${ID} -eq 0 ]; then
            ./server -sim=false -mode=blockbuilder -shard=${SHARD} -id=${ID} 2> error.log &
        else
            ./server -sim=false -mode=node -shard=${SHARD} -id=${ID} 2> error.log &
        fi
    fi
    echo $! >> ${SERVER_PID_FILE}
    echo "Start Protocol shard: ${SHARD}, id: ${ID}"
    echo "${alg} is running, on log severity: ${severity}"
    echo "=== if error occurs, plz check error.log ==="
else
    echo "Servers are already started in this folder."
    exit 0
fi