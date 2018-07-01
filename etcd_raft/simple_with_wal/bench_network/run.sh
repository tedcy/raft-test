dir=/root/raft_server
id=$1
peer1=$2
peer2=$3
peer3=$4
nohup $dir/main -id $id -peer1 $peer1 -peer2 $peer2 -peer3 $peer3 > $dir/log 2>&1 &
