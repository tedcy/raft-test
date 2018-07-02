package main

import (
	"time"
	"context"
	"coding.net/tedcy/sheep/src/common/bench"
	raft_node "coding.net/tedcy/raft-test/etcd_raft/simple_with_wal"
)

func InitFunc() (interface{}, []chan<- struct{}){
	peers := []string{
		"http://127.0.0.1:33333",
		"http://127.0.0.1:33334",
		"http://127.0.0.1:33335",
	}
	proposeC1 := make(chan []byte)
	proposeC2 := make(chan []byte)
	proposeC3 := make(chan []byte)
	_, _, _ = proposeC1, proposeC2, proposeC3
	commitC1 := raft_node.NewRaftNode(context.Background(), 1, peers,
		proposeC1, nil)
	commitC2 := raft_node.NewRaftNode(context.Background(), 2, peers,
		proposeC2, nil)
	commitC3 := raft_node.NewRaftNode(context.Background(), 3, peers,
		proposeC3, nil)

	//wait for replay
	<-commitC1
	<-commitC2
	<-commitC3

	//test
	commitC := make(chan []byte)
	var commitCs []<-chan []byte
	commitCs = append(commitCs, commitC1)
	commitCs = append(commitCs, commitC2)
	commitCs = append(commitCs, commitC3)
	for _, c := range commitCs {
		go func(c <-chan []byte) {
			for bs := range c {
				commitC <- bs
			}
		}(c)
	}
	go func() {
		for data := range commitC {
			if len(data) != len("hello raft") {
				panic(string(data))
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
	c := &bench.BenchConfig{}
	c.InitFunc = InitFunc
	c.BenchFunc = BenchFunc
	//c.Goroutines = []int{10,20,50,100}
	c.Time = 10 * time.Second
	bench.New(c).Run()
}
