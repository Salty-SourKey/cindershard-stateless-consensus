import os
import pandas as pd
import glob 
directory_path = './'

file_list = glob.glob('server*Experiment.csv')
df = pd.DataFrame()
result = {}
params = ["TotalTPS","LocalTPS","CrossTPS","LocalLatency","CrossLatency","PreCoordination","PreCoordinationWaiting","PreCoordinationConsensus","Coordination","CoordinationWaiting","CoordinationConsensus","Execute","ExecuteWaiting","ExecuteConsensus","TotalNetworkDelay","Total","SnapshotRatio"]

for file in file_list:
    log = pd.read_csv(file, delimiter=',')

    for param in params:
        if param not in result:
            result[param] = log.iloc[-1][param]
        else:
            result[param] += log.iloc[-1][param]


for param in params:
    df[param] = [result[param] / len(file_list)]

df["SumTotalTPS"] = result["TotalTPS"]

df.to_csv("TotalResult.csv", index=False)