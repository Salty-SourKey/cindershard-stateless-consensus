import os

region_instance = []

with open('instances.node.file', 'r') as file:
    for line in file:
        region_split = line.split(" ", 1)
        region = region_split[0]
        instances = region_split[1]
        instances = instances[2:-3]
        instances = instances.split(", ")
        for instance in instances:
            instance = instance[1:-1]
            region_instance.append(region+' '+instance)

# print(region_instance)
with open('instances.node.file', 'w') as file:
    for instance in region_instance:
        file.write(instance+"\n")