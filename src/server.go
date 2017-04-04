package server

import (
	"google.golang.org/grpc"
	log "github.com/golang/glog"
	"coding.net/tedcy/raft-test/src/proto/raft"
	"coding.net/tedcy/raft-test/src/config"
	"coding.net/tedcy/raft-test/src/data"
	"net"
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
	return time.Duration(RandRand.Int() % 300 + 150) * time.Millisecond
}

type raftClient struct {
	raft.RaftClient
	conn			*grpc.ClientConn
}

type termCheck struct {
	term			uint64
	vote			string
	leader			string
}

func (this *termCheck) checkVote(term uint64, vote string) bool {
	if term > this.term {
		this.term = term
		this.vote = vote
		return true
    }
	return false
}

func (this *termCheck) checkLeader(term uint64, leader string) bool {
	if term > this.term {
		this.term = term
		this.leader = leader
		return true
    }
	return false
}

type Server struct {
	status			Status
	term			uint64
	addrList		[]string
	clients			[]*raftClient
	myAdrr			string
	check			termCheck
	stopFollower	chan bool
	stopCandidate	chan bool
	t				*time.Timer
	grpcServer		*grpc.Server
	lis				net.Listener
	data			*data.Data
}

func (this *Server)	Vote(ctx context.Context, in *raft.VoteRequest) (*raft.VoteResponse, error) {
	var resp raft.VoteResponse
	log.Infof("vote req term %d addr %s",in.Term, in.Addr)
	if this.check.checkVote(in.Term, in.Addr) {
		resp.Ok = true
		this.stopFollower <- true
    }else {
		resp.Ok = false
	}
	log.Infof("vote resp %v",resp.Ok)
	return &resp, nil
}

func (this *Server) HeartBeat(ctx context.Context, in *raft.HeartBeatRequest) (*raft.HeartBeatResponse, error) {
	var resp raft.HeartBeatResponse
	log.Infof("heartbeat req term %d addr %s",in.Term, in.Addr)
	if this.check.checkLeader(in.Term, in.Addr) {
		this.stopCandidate <- true
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
	var stop bool
	for {
		switch this.status {
		case Follower:
			log.Info("into follower")
			this.t = time.NewTimer(electionTimeout())
			stop = false
			select {
				case <-this.stopFollower:
					stop = true
					this.t.Stop()
				case <-this.t.C:
			}
			if !stop {
				this.status = Candidate
            }
		case Candidate:
			log.Info("into candidate")
			var count int
			this.t = time.NewTimer(electionTimeout())
			stop = false
			select {
				case <-this.stopCandidate:
					stop = true
					this.t.Stop()
				case <-this.t.C:
			}
			if !stop {
				this.term++
				count++
				for _, s := range this.clients {
					req := &raft.VoteRequest{}
					req.Term = this.term
					req.Addr = this.myAdrr
					resp, err := s.Vote(context.TODO(), req)
					if err != nil {
						log.Error(err)
						continue
					}
					if resp.Ok {
						count++
					}
					if count > len(this.clients)/2{
						this.status = Leader
					}
				}
				log.Infof("voted %d",count)
            }else {
				this.status = Follower
            }
		case Leader:
			log.Info("into leader")
			this.t = time.NewTimer(20 * time.Millisecond)
			select {
				case <-this.t.C:
			}
			for _, s := range this.clients {
				req := &raft.HeartBeatRequest{}
				req.Term = this.term
				req.Addr = this.myAdrr
				_, err := s.HeartBeat(context.TODO(), req)
				if err != nil {
					log.Error(err)
					continue
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

	s.stopCandidate = make(chan bool)
	s.stopFollower = make(chan bool)

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
		s.clients = append(s.clients, client)
    }

	s.grpcServer = grpc.NewServer()
	raft.RegisterRaftServer(s.grpcServer, &s)
	return &s, nil
}

func (this *Server) Start() error {
	go this.houseKeeper()
	return this.grpcServer.Serve(this.lis)
}
