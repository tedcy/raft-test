package server

import (
	"coding.net/tedcy/raft-test/src/config"
	"coding.net/tedcy/raft-test/src/data"
	"coding.net/tedcy/raft-test/src/proto/raft"
	log "github.com/golang/glog"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"math/rand"
	"net"
	"sync"
	"time"
)

type Status int

const (
	Follower  = Status(0)
	Candidate = Status(1)
	Leader    = Status(2)
	//ElectionTick = 1000
	//HeartBeatTick = 100

	ElectionTick  = 150
	HeartBeatTick = 20
)

var RandRand *rand.Rand

func init() {
	RandRand = rand.New(rand.NewSource(int64(time.Now().Nanosecond())))
}

func electionTimeout() time.Duration {
	return time.Duration(RandRand.Intn(ElectionTick)+ElectionTick) * time.Millisecond
}

type raftClient struct {
	raft.RaftClient
	conn *grpc.ClientConn
	addr string
}

type raftState struct {
	term    uint64
	vote    string
	leader  string
	status  Status
	refresh bool
	sync.Mutex
}

func (this *raftState) wait(t time.Duration) bool {
	timer := time.NewTimer(t)
	this.Lock()
	this.refresh = false
	this.Unlock()
	select {
	case <-timer.C:
	}
	this.Lock()
	defer this.Unlock()
	if this.refresh {
		log.Info("wait end by refresh")
		return true
	}
	log.Info("wait end")
	return false
}

func (this *raftState) ifRefresh() bool {
	this.Lock()
	defer this.Unlock()
	return this.refresh
}

func (this *raftState) becomeFollower() {
	this.Lock()
	defer this.Unlock()
	log.Info("becomeFollower")
	this.status = Follower
}

func (this *raftState) becomeCandidate() {
	this.Lock()
	defer this.Unlock()
	log.Info("becomeCandidate")
	this.term++
	this.status = Candidate
}

func (this *raftState) holdCandidate() {
	this.Lock()
	defer this.Unlock()
	if this.status != Candidate {
		return
	}
	log.Info("holdCandidate")
	this.term++
}

func (this *raftState) becomeLeader() {
	this.Lock()
	defer this.Unlock()
	if this.status != Candidate {
		return
	}
	log.Info("becomeLeader")
	this.status = Leader
}

func (this *raftState) GetTerm() uint64 {
	return this.term
}

func (this *raftState) SetTerm(term uint64) {
	this.Lock()
	this.term = term
	this.Unlock()
}

func (this *raftState) Vote(term uint64, vote string) bool {
	this.Lock()
	defer this.Unlock()
	//如果in.term > this.term，那么更新follower的定时器，并且变成该addr的follower
	if term > this.term {
		this.term = term
		this.vote = vote
		//if this.status == Follower
		this.refresh = true
		log.Info("becomeFollower")
		this.status = Follower
		return true
	}
	return false
}

func (this *raftState) HeartBeat(term uint64, leader string) {
	this.Lock()
	defer this.Unlock()
	//如果in.term大于this.term，那么变成addr的follower，并且更新follower的定时器
	//如果in.term==this.term，那么更新follower的定时器
	if term > this.term {
		this.term = term
		this.leader = leader
		//if this.status == Follower
		this.refresh = true
		log.Info("becomeFollower")
		this.status = Follower
	}
	if term == this.term {
		//if this.status == Follower
		this.refresh = true
	}
}

type Server struct {
	addrList   []string
	clients    []*raftClient
	myAdrr     string
	raft       raftState
	t          *time.Timer
	grpcServer *grpc.Server
	lis        net.Listener
	data       *data.Data
}

func (this *Server) Vote(ctx context.Context, in *raft.VoteRequest) (*raft.VoteResponse, error) {
	var resp raft.VoteResponse
	log.Infof("vote myterm %d term %d addr %s", this.raft.term, in.Term, in.Addr)
	resp.Ok = this.raft.Vote(in.Term, in.Addr)
	return &resp, nil
}

func (this *Server) HeartBeat(ctx context.Context, in *raft.HeartBeatRequest) (*raft.HeartBeatResponse, error) {
	var resp raft.HeartBeatResponse
	log.Infof("heartbeat myterm %d term %d addr %s", this.raft.term, in.Term, in.Addr)
	this.raft.HeartBeat(in.Term, in.Addr)
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
	var ifRefresh bool
	for {
		switch this.raft.status {
		case Follower:
			log.Info("into follower")
			ifRefresh = this.raft.wait(electionTimeout())
			if !ifRefresh {
				this.raft.becomeCandidate()
			}
		case Candidate:
			log.Info("into candidate")
			count := 1
			var wg sync.WaitGroup
			var lock sync.Mutex
			for _, s := range this.clients {
				wg.Add(1)
				go func(s *raftClient) {
					defer wg.Done()
					req := &raft.VoteRequest{}
					req.Term = this.raft.GetTerm()
					req.Addr = this.myAdrr
					//ctx, _ := context.WithTimeout(context.Background(), 200*time.Millisecond)
					ctx := context.Background()
					resp, err := s.Vote(ctx, req)
					if err != nil {
						log.Errorf("vote to %s failed: %s", s.addr, err)
						return
					}
					if resp.Ok {
						lock.Lock()
						count++
						lock.Unlock()
					}
				}(s)
			}
			wg.Wait()
			log.Infof("voted %d", count)
			if count > len(this.clients)/2 {
				this.raft.becomeLeader()
			} else {
				this.t = time.NewTimer(electionTimeout())
				select {
				case <-this.t.C:
				}
				this.raft.holdCandidate()
			}
		case Leader:
			log.Info("into leader")
			for _, s := range this.clients {
				req := &raft.HeartBeatRequest{}
				req.Term = this.raft.GetTerm()
				req.Addr = this.myAdrr
				//ctx, _ := context.WithTimeout(context.Background(), 20*time.Millisecond)
				ctx := context.Background()
				_, err := s.HeartBeat(ctx, req)
				if err != nil {
					log.Error(err)
					continue
				}
			}
			this.t = time.NewTimer(HeartBeatTick * time.Millisecond)
			select {
			case <-this.t.C:
			}
		}
	}
}

func NewServer(c *config.Config) (*Server, error) {
	var err error
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
		client.addr = addr
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
