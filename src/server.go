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
	"sync"
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
	return time.Duration(RandRand.Int() % 150 + 150) * time.Millisecond
}

type raftClient struct {
	raft.RaftClient
	conn			*grpc.ClientConn
	addr			string
}

type termCheck struct {
	term			uint64
	vote			string
	leader			string
	status			Status
	sync.Mutex
}

func (this *termCheck) checkVote(term uint64, vote string) bool {
	if term > this.term {
		this.Lock()
		this.term = term
		this.Unlock()
		this.vote = vote
		return true
    }
	return false
}

func (this *termCheck) checkLeader(term uint64, leader string) bool {
	if term > this.term {
		this.Lock()
		this.term = term
		this.Unlock()
		this.leader = leader
		return true
    }
	return false
}

func (this *termCheck) becomeFollower() {
	this.Lock()
	defer this.Unlock()
	this.status = Follower
}

func (this *termCheck) becomeCandidate() {
	this.Lock()
	defer this.Unlock()
	if this.status == Leader {
		return
    }
	this.term++
	this.status = Candidate
}

func (this *termCheck) becomeLeader() {
	this.Lock()
	defer this.Unlock()
	this.status = Leader
}

func (this *termCheck) Get() uint64{
	return this.term
}

type TimerStoper struct {
	t				*time.Timer
	ifStop			bool
	sync.Mutex
}

func NewTimerStoper() *TimerStoper {
	var ts TimerStoper
	return &ts
}

func (this *TimerStoper) Set(t time.Duration) {
	this.t = time.NewTimer(t)
}

//if stop by chan, return ture
func (this *TimerStoper) Wait() bool{
	this.Lock()
	this.ifStop = false
	this.Unlock()
	select {
	case <-this.t.C:
		this.t.Stop()
		this.Lock()
		defer this.Unlock()
		if this.ifStop {
			log.Info("wait end by stop")
			return true
        }
		log.Info("wait end")
		return false
    }
}

func (this *TimerStoper) Stop() {
	this.Lock()
	defer this.Unlock()
	if !this.ifStop {
		this.ifStop = true
    }
}

type Server struct {
	addrList		[]string
	clients			[]*raftClient
	myAdrr			string
	check			termCheck
	stopFollower	*TimerStoper
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
		this.stopFollower.Stop()
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
		this.check.becomeFollower()
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
		switch this.check.status {
		case Follower:
			log.Info("into follower")
			this.stopFollower.Set(electionTimeout())
			stop = this.stopFollower.Wait()
			if !stop {
				this.check.becomeCandidate()
            }
		case Candidate:
			log.Info("into candidate")
			t := time.Now()
			for {
				count := 1
				var wg sync.WaitGroup
				var lock sync.Mutex
				for _, s := range this.clients {
					wg.Add(1)
					go func(s *raftClient) {
						defer wg.Done()
						req := &raft.VoteRequest{}
						req.Term = this.check.Get()
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
				log.Infof("voted %d",count)
				if count > len(this.clients)/2 && this.check.status == Candidate{
					this.check.becomeLeader()
					break
				}
				if this.check.status == Follower {
					break
                }
				if time.Duration(time.Since(t).Nanoseconds()) > 100 * time.Millisecond{
					this.check.becomeCandidate()
					break
                }
				select {
				case <-time.After(10 * time.Millisecond):
				}
			}
		case Leader:
			log.Info("into leader")
			this.t = time.NewTimer(20 * time.Millisecond)
			select {
			case <-this.t.C:
			}
			for _, s := range this.clients {
				req := &raft.HeartBeatRequest{}
				req.Term = this.check.Get()
				req.Addr = this.myAdrr
				//ctx, _ := context.WithTimeout(context.Background(), 20*time.Millisecond)
				ctx := context.Background()
				_, err := s.HeartBeat(ctx, req)
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

	s.stopFollower = NewTimerStoper()

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
