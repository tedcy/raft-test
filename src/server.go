package server

import (
	"google.golang.org/grpc"
	log "github.com/golang/glog"
	"coding.net/tedcy/raft-test/src/proto/raft"
	"coding.net/tedcy/raft-test/src/config"
	"coding.net/tedcy/raft-test/src/data"
	"net"
	"sync"
	"time"
	"math/rand"
	"golang.org/x/net/context"
)

type Status int

const (
	Follower = Status(0)
	Candidate = Status(1)
	Leader = Status(2)
)

var RandRand *rand.Rand 

func init() {
	RandRand = rand.New(rand.NewSource(int64(time.Now().Nanosecond())))
}

func electionTimeout() time.Duration {
	return time.Duration((RandRand.Int() % 300)* 2) * time.Millisecond
}

type VotedCount struct {
	count		int
	sync.Mutex
}

func (this *VotedCount) Add() {
	this.Lock()
	this.count++
	this.Unlock()
}

func (this *VotedCount) Clean() {
	this.Lock()
	this.count = 0
	this.Unlock()
}

func (this *VotedCount) Get() int{
	this.Lock()
	defer this.Unlock()
	return this.count
}

type raftClient struct {
	raft.RaftClient
	conn			*grpc.ClientConn
}

type Server struct {
	status			Status
	term			uint64
	votedCount		VotedCount
	addrList		[]string
	servers			[]*raftClient
	myAdrr			string
	leader			string
	vote			string
	alive			chan bool
	t				*time.Timer
	grpcServer		*grpc.Server
	lis				net.Listener
	data			*data.Data
}

func (this *Server)	Vote(ctx context.Context, in *raft.VoteRequest) (*raft.VoteResponse, error) {
	var resp raft.VoteResponse
	this.term = in.Term
	if this.votedCount.Get() != 0 {
		resp.Ok = false
		return &resp, nil
    }
	if this.vote != "" {
		resp.Ok = false
		return &resp, nil
    }
	this.vote = in.Addr
	resp.Ok = true
	return &resp, nil
}

func (this *Server) HeartBeat(ctx context.Context, in *raft.HeartBeatRequest) (*raft.HeartBeatResponse, error) {
	var resp raft.HeartBeatResponse

	if in.Term > this.term {
		if this.status == Candidate {
			this.status = Follower
			this.leader = in.Addr
        }
    }
	return &resp, nil
}

func (this *Server) Set(ctx context.Context, in *raft.SetRequest) (*raft.SetResponse, error) {
	var err error
	var resp raft.SetResponse
	err = this.data.Set(in.Key, in.Value)
	if err != nil {
		return nil, err
    }
	return &resp, nil
}

func (this *Server) Get(ctx context.Context, in *raft.GetRequest) (*raft.GetResponse, error) {
	var err error
	var resp raft.GetResponse
	resp.Value, err = this.data.Get(in.Key)
	if err != nil {
		return nil, err
    }
	return &resp, nil
}

func (this *Server) houseKeeper() {
	var alive bool
	for {
		this.t = time.NewTimer(electionTimeout())
		switch this.status {
		case Follower:
			alive = false
			select {
				case <-this.alive:
					alive = true
				case <-this.t.C:
			}
			if !alive {
				this.status = Candidate
				this.votedCount.Add()
            }
		case Candidate:
			for _, s := range this.servers {
				req := &raft.VoteRequest{}
				req.Term = this.term
				req.Addr = this.myAdrr
				resp, err := s.Vote(context.TODO(), req)
				if err != nil {
					log.Error(err)
                }
				if resp.Ok {
					this.votedCount.Add()
                }
				if this.votedCount.Get() > len(this.servers){
					this.status = Leader
                }
            }
		case Leader:
			this.votedCount.Clean()
			for _, s := range this.servers {
				req := &raft.HeartBeatRequest{}
				req.Term = this.term
				req.Addr = this.myAdrr
				_, err := s.HeartBeat(context.TODO(), req)
				if err != nil {
					log.Error(err)
                }
            }
        }
    }
}

func NewServer(c *config.Config) (*Server, error){
	var err	error
	var s Server

	s.lis, err = net.Listen("tcp", c.MyAddr)
	if err != nil {
		return nil, err
    }
	s.myAdrr = c.MyAddr

	s.data, err = data.NewData()
	if err != nil {
		return nil, err
    }

	s.alive = make(chan bool)

	s.addrList = c.Addr
	for _, addr := range s.addrList {
		conn, err := grpc.Dial(addr, grpc.WithInsecure())
		if err != nil {
			return nil, err
        }
		c := raft.NewRaftClient(conn)
		client := &raftClient{}
		client.RaftClient = c
		client.conn = conn
    }

	grpcServer := grpc.NewServer()
	raft.RegisterRaftServer(grpcServer, &s)
	return &s, nil
}

func (this *Server) Start() error {
	return this.grpcServer.Serve(this.lis)
}
