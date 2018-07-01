package raft_node

import (
	"context"
	"testing"
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
	commitC := NewRaftNode(context.Background(), 1, peers,
		proposeC1, nil)
	_ = NewRaftNode(context.Background(), 2, peers,
		proposeC2, nil)
	_ = NewRaftNode(context.Background(), 3, peers,
		proposeC3, nil)
	proposeC3 <- []byte("hello raft")
	bs := <-commitC
	println(string(bs))
}
