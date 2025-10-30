#!/usr/bin/env bash
start=`date +%s.%N`

VARIABLE=config.conf

K=1
while read line
do
    if [ ${K} -eq 1 ]; then
        SHARD=$(echo $line | cut -d ":" -f 2)
        SHARD=$(echo $SHARD | cut -d " " -f 2)
    elif [ ${K} -eq 2 ]; then
        COMMITTEE=$(echo $line | cut -d ":" -f 2)
        COMMITTEE=$(echo $COMMITTEE | cut -d " " -f 2)
    fi
    K=$((K+1))
done < $VARIABLE
echo "Shard: ${SHARD}, Committee: ${COMMITTEE}"
SHARD=$((SHARD+1))
TOTALCOMMITTEE=$((SHARD*COMMITTEE))

distribute(){
    SERVER_ADDR=(`cat ../../common/base_ips.txt`)
    echo "BlockBuilder Update config"
    for (( j=0; j<=${SHARD}; j++))
    do 
        printf "Update ${SERVER_ADDR[j]}\n"
       ./_update_cmd.sh ubuntu ${SERVER_ADDR[j]} 1>>../../log/update.log 2>>../../log/update_err.log &
       sleep 0.3s
    done
    NODE_ADDR=(`cat ../../common/ips.txt`)
    echo "Node Update config"
    for (( j=0; j<${TOTALCOMMITTEE}; j++))
    do 
        printf "Update ${NODE_ADDR[j]}\n"
       ./_update_cmd.sh ubuntu ${NODE_ADDR[j]} 1>>../../log/update.log 2>>../../log/update_err.log &
       sleep 0.3s
    done
}

rm -rf log/deploy*
# distribute files
distribute

finish=`date +%s.%N`
diff=$( echo "$finish - $start" | bc -l )
echo 'start:' $start
echo 'finish:' $finish
echo 'diff:' $diff
