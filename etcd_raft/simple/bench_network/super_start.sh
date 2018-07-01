set -x
server=("172.17.32.174" "172.17.32.175" "172.16.186.221")
for ((i=1;i<=3;i++));do
    ./bstart.sh ${server[$i-1]} $i ${server[0]} ${server[1]} ${server[2]}
done
