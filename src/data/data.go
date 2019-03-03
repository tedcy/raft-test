package data

import (
	"sync"
)

type Data struct {
	data map[string]string
	sync.Mutex
}

func NewData() (*Data, error) {
	var d Data
	d.data = make(map[string]string)
	return &d, nil
}

func (this *Data) Get(key string) (string, error) {
	this.Lock()
	value := this.data[key]
	this.Unlock()
	return value, nil
}

func (this *Data) Set(key, value string) error {
	this.Lock()
	this.data[key] = value
	this.Unlock()
	return nil
}
