package raft_node

import (
	"context"
	"testing"
	"time"
)

func Test_Demo(t *testing.T) {
	peers := []string{
		"http://127.0.0.1:33333",
		"http://127.0.0.1:33334",
		"http://127.0.0.1:33335",
	}
	proposeC1 := make(chan []byte)
	proposeC2 := make(chan []byte)
	proposeC3 := make(chan []byte)
	_, _, _ = proposeC1, proposeC2, proposeC3
	commitC1 := NewRaftNode(context.Background(), 1, peers,
		proposeC1, nil)
	commitC2 := NewRaftNode(context.Background(), 2, peers,
		proposeC2, nil)
	commitC3 := NewRaftNode(context.Background(), 3, peers,
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
		go func() {
			for bs := range c {
				commitC <- bs
			}
		}()
	}
	go func() {
		for bs := range commitC {
			println("msg:", string(bs))
		}
	}()
	proposeC1 <- []byte("hello raft1")
	proposeC1 <- []byte("hello raft2")
	time.Sleep(1000 * time.Second)
}
