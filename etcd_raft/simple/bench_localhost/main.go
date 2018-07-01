package main

import (
	"time"
	"context"
	"coding.net/tedcy/sheep/src/common/bench"
	raft_node "coding.net/tedcy/raft-test/etcd_raft/simple"
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
	commitC := raft_node.NewRaftNode(context.Background(), 1, peers,
		proposeC1, nil)
	_ = raft_node.NewRaftNode(context.Background(), 2, peers,
		proposeC2, nil)
	_ = raft_node.NewRaftNode(context.Background(), 3, peers,
		proposeC3, nil)
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
	c := &bench.BenchConfig{}
	c.InitFunc = InitFunc
	c.BenchFunc = BenchFunc
	//c.Goroutines = []int{10,20,50,100}
	c.Time = 10 * time.Second
	bench.New(c).Run()
}