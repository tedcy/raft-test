package main

import (
	"flag"
	"time"
	"context"
	"coding.net/tedcy/sheep/src/common/bench"
	raft_node "coding.net/tedcy/raft-test/etcd_raft/simple"
)

var (
	id *int = flag.Int("id",0,"")
	peer1 *string = flag.String("peer1","127.0.0.1","")
	peer2 *string = flag.String("peer2","127.0.0.1","")
	peer3 *string = flag.String("peer3","127.0.0.1","")
)

func InitFunc() (interface{}, []chan<- struct{}){
	peers := []string{
		"http://" + *peer1 + ":33333",
		"http://" + *peer2 + ":33333",
		"http://" + *peer3 + ":33333",
	}
	proposeC1 := make(chan []byte)
	commitC := raft_node.NewRaftNode(context.Background(), *id, peers,
		proposeC1, nil)
	go func() {
		for data := range commitC {
			if len(data) != len("hello raft") {
				panic("")
			}
		}
	}()
	return proposeC1, nil
}

func BenchFunc(data interface{}) error {
	proposeC := data.(chan []byte)
	proposeC <- []byte("hello raft")
	return nil
}

func main() {
	flag.Parse()
	if *id == 1 {
		c := &bench.BenchConfig{}
		c.InitFunc = InitFunc
		c.BenchFunc = BenchFunc
		//c.Goroutines = []int{10,20,50,100}
		c.Time = 10 * time.Second
		bench.New(c).Run()
	}else {
		InitFunc()
		c := make(chan struct{})
		<-c
	}
}
