package main

import (
	server "coding.net/tedcy/raft-test/src"
	log "github.com/golang/glog"
	"coding.net/tedcy/raft-test/src/config"
	"github.com/BurntSushi/toml"
)

func main() {
	var c config.Config
	_, err := toml.DecodeFile("server.toml",&c)
	if err != nil {
		log.Error(err)
		return
    }
	s, err := server.NewServer(&c)
	if err != nil {
		log.Error(err)	
		return
    }
	err = s.Start()
	if err != nil {
		log.Error(err)
		return
    }
}
