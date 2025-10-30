#!/usr/bin/env bash
# if less than two arguments supplied, display usage
# if [  $# -le 0 ]
# then
#     display_usage
#     echo -e "Error: algoirthm not given"
#     exit 1
# fi
display_usage() {
    echo -e "\nUsage: $0 [-h] [-a algorithm] [-s severity] \n"
    echo "  -h              display help"
    echo "  -a algorithm    specify consensus: [pbft, falcon, xft, zyzzyva]"
    echo "  -m mode         specify mode: [client, coordination, worker]"
    echo "  -s severity     specify severity, default is 'debug': [info, debug, warning]"
}
# check whether user had supplied -h or --help . If yes display usage
if [[ ( $@ == "--help") ||  $@ == "-h" ]]
then
    display_usage
    exit 2
fi
alg=pbft
severity=debug
mode=coordination
while getopts a:m:s:h: flag
do
    case "${flag}" in
        a) alg=${OPTARG} ;;
        m) mode=${OPTARG} ;;
        s) severity=${OPTARG} ;;
        h) ;;
    esac
done
if [[ ${alg} != "pbft" && ${alg} != "falcon" && ${alg} != "xft" && ${alg} != "zyzzyva" && ${alg} != "raft" && ${alg} != "paxos" ]]; then
    display_usage
    echo -e "Error: invalid algoirthm ${alg}"
    exit 4
fi
if [[ ${severity} != "debug" && ${severity} != "info" && ${severity} != "warning" ]]; then
    display_usage
    echo -e "Error: invalid severity ${severity}"
    exit 5
fi
if [[ ${mode} != "client" && ${mode} != "coordination" && ${mode} != "worker" ]]; then
    display_usage
    echo -e "Error: invalid mode ${mode}"
    exit 6
fi
if [ ${mode} == "client" ]; then
    PID_FILE=client.pid
    PID=$(cat "${PID_FILE}");
    if [ -z "${PID}" ]; then
        echo "Process id for clients is written to location: {$PID_FILE}"
        ./client&
        echo $! >> ${PID_FILE}
    else
        echo "Clients are already started in this folder."
        exit 0
    fi
elif [[ ${mode} == "coordination" || ${mode} == "worker" || ${mode} == "gateway" ]]; then
    SERVER_PID_FILE=server.pid
    SERVER_PID=$(cat "${SERVER_PID_FILE}");
    if [ -z "${SERVER_PID}" ]; then
        echo "Process id for servers is written to location: {$SERVER_PID_FILE}"
        ./main -sim=true -algorithm=${alg} -mode=${mode} 2> error.log &
        echo $! >> ${SERVER_PID_FILE}
        echo "${alg} is running, on log severity: ${severity}"
        echo "=== if error occurs, plz check error.log ==="
    else
        echo "Servers are already started in this folder."
        exit 0
    fi
fi