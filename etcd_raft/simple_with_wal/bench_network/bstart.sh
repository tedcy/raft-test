set -x
dir=/root/raft_server
ip=$1
id=$2
peer1=$3
peer2=$4
peer3=$5
ssh $ip "killall main"
ssh $ip "rm -rf wal-*"
#go build main.go && ssh $ip mkdir -pv $dir && scp main root@$ip:$dir && scp run.sh root@$ip:$dir && ssh $ip $dir/run.sh $id $peer1 $peer2 $peer3 >/dev/null 2>&1 &
go build main.go && ssh $ip mkdir -pv $dir && scp main root@$ip:$dir && scp run.sh root@$ip:$dir && ssh $ip $dir/run.sh $id $peer1 $peer2 $peer3
