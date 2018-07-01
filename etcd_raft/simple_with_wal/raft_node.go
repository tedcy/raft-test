package raft_node

import (
	"fmt"
	"os"
	"time"
	"context"
	"strconv"
	"github.com/coreos/etcd/raft"
	"github.com/coreos/etcd/raft/raftpb"
	"github.com/coreos/etcd/etcdserver/stats" //for rafthttp
	"github.com/coreos/etcd/pkg/types"	//for rafthttp
	"net"								//for rafthttp
	"net/url"							//for rafthttp
	"net/http"							//for rafthttp
	"github.com/coreos/etcd/rafthttp"	//不使用的话实现很复杂
	"github.com/coreos/etcd/wal"
	"github.com/coreos/etcd/wal/walpb"
)

type RaftNode struct {
	//外部输入
	proposeC		<-chan []byte
	confChangeC		<-chan raftpb.ConfChange
	//内部raft输出
	commitC			chan<- []byte
	//nouse here
	//errorC		chan<- error

	//配置信息
	id				int
	walDir			string
	peers			[]string

	//内部状态
	appliedIndex	uint64
	lastReplayIndex uint64

	//raft相关模块
	node			raft.Node
	raftStorage		*raft.MemoryStorage
	transport		*rafthttp.Transport
	wal				*wal.WAL
	
	ctx				context.Context
	cancel			context.CancelFunc
}

func NewRaftNode(ctx context.Context, id int, peers []string, proposeC <-chan []byte, 
	confChangeC <-chan raftpb.ConfChange) (commitC <-chan []byte){

	newCommitC := make(chan []byte)
	commitC = newCommitC
	
	rn := new(RaftNode)
	rn.ctx, rn.cancel = context.WithCancel(ctx)
	rn.id = id
	rn.proposeC = proposeC
	rn.confChangeC = confChangeC
	rn.commitC = newCommitC
	rn.peers = peers

	//必须是异步的，否则会阻塞在<-commitC
	go rn.startRaft()
	return
}

func (this *RaftNode) startRaft() {
	//wal
	//raftstorage在这个过程初始化
	this.walDir = fmt.Sprintf("wal-%d",this.id)
	oldWal := wal.Exist(this.walDir)
	this.wal = this.replayWAL()

	//node
	c := &raft.Config{}
	c.ID = uint64(this.id)
	c.ElectionTick = 10
	c.HeartbeatTick = 1
	c.Storage = this.raftStorage
	//??
	c.MaxSizePerMsg = 1024*1024
	c.MaxInflightMsgs = 256

	//如果是重启，peer信息可以从wal中恢复
	if oldWal {
		this.node = raft.RestartNode(c)
	}else {
		rpeers := make([]raft.Peer, len(this.peers))
		for i := range rpeers {
			rpeers[i] = raft.Peer{ID: uint64(i + 1)}
		}
		this.node = raft.StartNode(c, rpeers)
	}
	
	//transport
	this.transport = &rafthttp.Transport{}
	this.transport.ID = types.ID(this.id)
	this.transport.ClusterID = 0x1000
	this.transport.Raft = this;//TODO 需要实现接口
	this.transport.ErrorC = make(chan error)
	//??
	this.transport.ServerStats = stats.NewServerStats("", "")
	this.transport.LeaderStats = stats.NewLeaderStats(strconv.Itoa(this.id))
	this.transport.Start()

	for i, peer := range this.peers {
		if i + 1 != this.id {
			this.transport.AddPeer(types.ID(i + 1), []string{peer})
		}
	}

	//开启rafthttp接口
	go this.RaftHttpStart(this.peers[this.id - 1])
	//开启raft事件循环
	go this.LooperStart()

	return
}

func (this *RaftNode) LooperStart() {
	//外部输入
	//输入通过node的propose接口会进入到内部raft事件循环的状态机
	go func() {
		for {
			select {
			case prop, ok := <-this.proposeC:
				if ok {
					this.node.Propose(context.TODO(), prop)
				}

			case confChange, ok := <-this.confChangeC:
				if ok {
					this.node.ProposeConfChange(context.TODO(), confChange)
				}
			}
		}
	}()
	
	//内部raft输入事件循环
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			this.node.Tick()

		case rd := <-this.node.Ready():
			//HardState,Entries和Snapshot写入storage
			//主逻辑：
			//发送信息给从
			//信息会包含entries和commit，所以从会同时写entries和进行commit
			//发送成功后commit
			//从逻辑：
			//commit新的数据
			for _, ent := range rd.Entries {
				println(ent.Type.String(), string(ent.Data))
			}
			for _, ent := range rd.CommittedEntries {
				println("commit", ent.Type.String(), string(ent.Data))
			}
			this.wal.Save(rd.HardState, rd.Entries)
			//这部分append是raft系统用于状态机同步的
			this.raftStorage.Append(rd.Entries)
			this.transport.Send(rd.Messages)
			ents := this.entsToApply(rd.CommittedEntries)
			//更新已经被commit的数据到状态机
			//更新节点
			this.applyChange(ents)
			this.node.Advance()
		}
	}

}

//过滤已经接收过的
//ents 3,4,5,6, has applied 5, [3:]就是6,也就是只要5以后的
func (this *RaftNode) entsToApply(ents []raftpb.Entry) (nents []raftpb.Entry) {
	if len(ents) == 0 {
		return
	}
	firstIndex := ents[0].Index
	if firstIndex > this.appliedIndex + 1{
		panic("err")
	}
	if this.appliedIndex + 1 - firstIndex < uint64(len(ents)) {
		nents = ents[this.appliedIndex + 1 - firstIndex:]
	}
	return
}

func (this *RaftNode) applyChange(ents []raftpb.Entry) {
	for _, ent := range ents {
		switch ent.Type {
		case raftpb.EntryNormal:
			if ent.Data == nil {
				//ignore
				break
			}
			this.commitC <- ent.Data
		case raftpb.EntryConfChange:
			var cc raftpb.ConfChange
			cc.Unmarshal(ent.Data)
			//踩坑
			//这边必须apply，否则回放的时候会导致propose阻塞
			this.node.ApplyConfChange(cc)
			//confState??
			switch cc.Type {
			case raftpb.ConfChangeAddNode:
				if cc.Context != nil {
					this.transport.AddPeer(types.ID(cc.NodeID), 
							[]string{string(cc.Context)})
				}
			case raftpb.ConfChangeRemoveNode:
				if cc.NodeID == uint64(this.id) {
					panic("err")
				}
				this.transport.RemovePeer(types.ID(cc.NodeID))
			}
		}
		this.appliedIndex = ent.Index

		//见replayWAL
		if this.lastReplayIndex == ent.Index {
			this.commitC <- nil
		}
	}
}

func (this *RaftNode) replayWAL() *wal.WAL{
	if !wal.Exist(this.walDir) {
		err := os.Mkdir(this.walDir, 0750)
		if err != nil{
			panic(err)
		}
		w, err := wal.Create(this.walDir, nil)
		if err != nil {
			panic(err)
		}
		w.Close()
	}
	//nouse snap
	walsnap := walpb.Snapshot{}
	w, err := wal.Open(this.walDir, walsnap)
	if err != nil {
		panic(err)
	}
	_, st, ents, err := w.ReadAll()
	if err != nil {
		panic(err)
	}
	this.raftStorage = raft.NewMemoryStorage()
	this.raftStorage.SetHardState(st)
	this.raftStorage.Append(ents)
	if len(ents) > 0 {
		//必须回放完所有的wal才可以写数据
		//这里记录index，后面匹配到这个index就说明回放完成了
		this.lastReplayIndex = ents[len(ents)-1].Index
	}else {
		//没有可以回放的wal，通知可以写数据了
		this.commitC <- nil
	}
	return w
}

func (this *RaftNode) RaftHttpStart(peer string) {
	u, err := url.Parse(peer)
	if err != nil {
		panic(err)
	}
	listener, err := net.Listen("tcp", u.Host)
	if err != nil {
		panic(err)
	}
	err = (&http.Server{Handler: this.transport.Handler()}).Serve(listener)
	if err != nil {
		panic(err)
	}
}

func (this *RaftNode) Stop() error {
	this.cancel()
	return nil
}

//为了rafthttp实现的接口
func (this *RaftNode) Process(ctx context.Context, m raftpb.Message) error {
	return this.node.Step(ctx, m)
}
func (this *RaftNode) IsIDRemoved(id uint64) bool                           { return false }
func (this *RaftNode) ReportUnreachable(id uint64)                          {}
func (this *RaftNode) ReportSnapshot(id uint64, status raft.SnapshotStatus) {}
