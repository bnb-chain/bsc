#!/bin/bash

# profile url, example: http://localhost:6060/debug/pprof/heap
url=$1
# interval in seconds to take profile, example: 300
interval=$2
# file name prefix, example: heap_
prefix=$3

echo "url="${url}
echo "interval="${interval}" seconds"
echo "prefix="${prefix}

for i in $(seq 1 200)
do
time=$(date "+%Y%m%d-%H%M%S")
echo "Capturing "${i}"-th profile at "${time}"....."
curl ${url} > ${prefix}${i}"_"${time}.pprof
sleep ${interval}
done

echo "Done!"