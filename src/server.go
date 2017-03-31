package server

import (
	log "github.com/golang/glog"
)

type Status int

const (
	Follower = Status(0)
	Candidate = Status(1)
	Leadera = Status(2)
)

type Server struct {

}
