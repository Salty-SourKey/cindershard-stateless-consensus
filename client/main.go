package main

import (
	"flag"
	"net/http"
	"unishard/client/benchmark"
	"unishard/client/client"
	"unishard/client/db"
	"unishard/config"
	"unishard/types"
)

// Database implements bamboo.DB interface for benchmarking
type Database struct {
	client.Client
}

func (d *Database) Init() error {
	return nil
}

func (d *Database) Stop() error {
	return nil
}

func (d *Database) Write(k int, v []byte, shards []types.Shard) error {
	key := db.Key(k)
	err := d.Put(key, v, shards)
	return err
}

// Send Transaction to Gateway from Client
func (d *Database) GatewayWrite(k int, v []byte) error {
	key := db.Key(k)
	err := d.GatewayPut(key, v)
	return err
}

func Init() {
	flag.Parse()
	// log.Setup()
	config.Configuration.Load()
	http.DefaultTransport.(*http.Transport).MaxIdleConnsPerHost = 1000
}

func main() {
	Init()

	d := new(Database)
	d.Client = client.NewHTTPClient()
	b := benchmark.NewBenchmark(d)
	if b == nil {
		return
	}
	b.Run()
}
