package client

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httputil"
	"strconv"
	"sync"

	"unishard/client/db"
	"unishard/config"
	"unishard/log"
	"unishard/node"
	"unishard/types"
	"unishard/utils"
)

// Client interface provides get and put for key value store
type Client interface {
	Get(db.Key) (string, error)
	Put(db.Key, db.Value, []types.Shard) error
	GatewayPut(db.Key, db.Value) error
}

// AdminClient interface provides fault injection opeartion
type AdminClient interface {
	Consensus(db.Key) bool
	Crash(types.NodeID, int)
	Drop(types.NodeID, types.NodeID, int)
	Partition(int, ...types.NodeID)
}

// HTTPClient implements Client interface with REST API
type HTTPClient struct {
	Addrs map[types.Shard]map[types.NodeID]string
	HTTP  map[types.NodeID]string
	ID    types.NodeID // client id use the same id as servers in local site
	N     int          // total number of nodes

	CID int // command id
	*http.Client
}

// NewHTTPClient creates a new Client from config
func NewHTTPClient() *HTTPClient {
	c := &HTTPClient{
		N:      len(config.Configuration.Addrs),
		Addrs:  config.Configuration.Addrs,
		HTTP:   config.Configuration.HTTPAddrs,
		Client: &http.Client{},
	}
	// will not send request to Byzantine nodes
	bzn := config.GetConfig().ByzNo
	if config.GetConfig().Strategy == "silence" {
		for i := 1; i <= bzn; i++ {
			id := utils.NewNodeID(i)
			shard := config.Configuration.GetShardNumOfID(id)
			delete(c.Addrs[shard], id)
			delete(c.HTTP, id)
		}
	}
	return c
}

// Get gets value of given key (use REST)
// Default implementation of Client interface
func (c *HTTPClient) Get(key db.Key) (string, error) {
	c.CID++
	//v, _, err := c.RESTGet(key)
	//return v, err
	return "", nil
}

// Put puts new key value pair and return previous value (use REST)
// Default implementation of Client interface
func (c *HTTPClient) Put(key db.Key, value db.Value, shards []types.Shard) error {
	c.CID++
	return c.RESTPut(key, value, shards)
}

func (c *HTTPClient) GatewayPut(key db.Key, value db.Value) error {
	c.CID++
	return c.GatewayRESTPut(key, value)
}

func (c *HTTPClient) WorkerBlockBuilderPut(shard types.Shard, value []byte) error {
	c.CID++
	return c.WorkerBlockBuilderRESTPut(shard, value)
}

//func (c *HTTPClient) GetURL(key db.Key) (types.NodeID, string) {
//	replicaID := config.GetConfig().Master
//	if replicaID == "0" {
//		keys := reflect.ValueOf(c.HTTP).MapKeys()
//		replicaID = keys[rand.Intn(len(keys))].Interface().(types.NodeID)
//	}
//	return replicaID, c.HTTP[replicaID] + "/" + strconv.Itoa(int(key))
//}

// rest accesses server's REST API with url = http://ip:port/key
// if value == nil, it's a read
func (c *HTTPClient) rest(url string, value db.Value) error {
	method := http.MethodGet
	var body io.Reader
	if value != nil {
		method = http.MethodPut
		body = bytes.NewBuffer(value)
	}
	//v, _ := io.ReadAll(body)
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		log.Error(err)
		return err
	}
	req.Header.Set(node.HTTPClientID, string(c.ID))
	req.Header.Set(node.HTTPCommandID, strconv.Itoa(c.CID))
	req.Header.Set("Connection", "keep-alive")
	//log.Debugf("The payload is %x",)

	rep, err := c.Client.Do(req)
	if err != nil {
		log.Error(err)
		return err
	}
	defer rep.Body.Close()

	if rep.StatusCode == http.StatusOK {
		return nil
	}

	// http call failed
	dump, _ := httputil.DumpResponse(rep, true)
	log.Debugf("%q", dump)
	return errors.New(rep.Status)
}

// RESTGet issues a http call to node and return value and headers
//func (c *HTTPClient) RESTGet(key db.Key) (string, error) {
//	return c.rest(key, nil)
//}

// RESTPut puts new value as http.request body and return previous value
func (c *HTTPClient) RESTPut(key db.Key, value db.Value, shards []types.Shard) error {
	if shards != nil {
		var wait sync.WaitGroup
		var err error
		for _, shard := range shards {
			wait.Add(1)
			go func(shard types.Shard) {
				err = c.ABlockBuilderPut(key, value, shard)
				wait.Done()
			}(shard)
		}
		wait.Wait()
		return err
	}
	return c.AllPut(key, value)
}

func (c *HTTPClient) GatewayRESTPut(key db.Key, value db.Value) error {
	return c.AllGatewayPut(key, value)
}

func (c *HTTPClient) WorkerBlockBuilderRESTPut(shard types.Shard, value []byte) error {
	return c.AllWorkerBlockBuilderPut(shard, value)
}

func (c *HTTPClient) json(id types.NodeID, key db.Key, value db.Value) (db.Value, error) {
	url := c.HTTP[id]
	cmd := db.Command{
		Key:       key,
		Value:     value,
		ClientID:  c.ID,
		CommandID: c.CID,
	}
	data, _ := json.Marshal(cmd)
	res, err := c.Client.Post(url, "json", bytes.NewBuffer(data))
	if err != nil {
		log.Error(err)
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusOK {
		b, _ := io.ReadAll(res.Body)
		log.Debugf("key=%v value=%x", key, db.Value(b))
		return db.Value(b), nil
	}
	dump, _ := httputil.DumpResponse(res, true)
	log.Debugf("%q", dump)
	return nil, errors.New(res.Status)
}

// JSONGet posts get request in json format to server url
func (c *HTTPClient) JSONGet(key db.Key) (db.Value, error) {
	return c.json(c.ID, key, nil)
}

// JSONPut posts put request in json format to server url
func (c *HTTPClient) JSONPut(key db.Key, value db.Value) (db.Value, error) {
	return c.json(c.ID, key, value)
}

//// QuorumGet concurrently read values from majority nodes
//func (c *HTTPClient) QuorumGet(key db.Key) ([]string, []map[string]string) {
//	return c.MultiGet(c.N/2+1, key)
//}

// MultiGet concurrently read values from n nodes
//func (c *HTTPClient) MultiGet(n int, key db.Key) ([]string, []map[string]string) {
//	valueC := make(chan string)
//	metaC := make(chan map[string]string)
//	i := 0
//	for id := range c.HTTP {
//		go func(id types.NodeID) {
//			v, meta, err := c.rest(key, nil)
//			if err != nil {
//				log.Error(err)
//				return
//			}
//			valueC <- v
//			metaC <- meta
//		}(id)
//		i++
//		if i >= n {
//			break
//		}
//	}
//
//	values := make([]string, 0)
//	metas := make([]map[string]string, 0)
//	for ; i > 0; i-- {
//		values = append(values, <-valueC)
//		metas = append(metas, <-metaC)
//	}
//	return values, metas
//}

// QuorumPut concurrently write values to majority of nodes
// TODO get headers
func (c *HTTPClient) AllPut(key db.Key, value db.Value) error {
	var wait sync.WaitGroup
	var err error
	for id, ip := range c.HTTP {
		wait.Add(1)
		go func(id int, ip string) {
			url := ip + "/" + strconv.Itoa(int(key)+id)
			err = c.rest(url, value)
			wait.Done()
		}(utils.Node(id), ip)
	}
	wait.Wait()
	return err
}

func (c *HTTPClient) AShardPut(key db.Key, value db.Value, shard types.Shard) error {
	var wait sync.WaitGroup
	var err error
	for id, ip := range c.HTTP {
		if config.Configuration.GetShardNumOfID(id) == shard {
			wait.Add(1)
			go func(id int, ip string) {
				url := ip + "/" + strconv.Itoa(int(key)+id)
				err = c.rest(url, value)
				wait.Done()
			}(utils.Node(id), ip)
		}
	}
	wait.Wait()
	return err
}

func (c *HTTPClient) ABlockBuilderPut(key db.Key, value db.Value, shard types.Shard) error {
	var wait sync.WaitGroup
	var err error

	ip := "http://127.0.0.1:8000"

	wait.Add(1)
	go func(shard types.Shard, ip string) {
		url := ip
		err = c.rest(url, value)
		wait.Done()
	}(shard, ip)

	wait.Wait()
	return err
}

func (c *HTTPClient) AllGatewayPut(key db.Key, value db.Value) error {
	var wait sync.WaitGroup
	var err error

	ip := "http://127.0.0.1:8000"

	wait.Add(1)
	go func(ip string) {
		url := ip + "/"
		err = c.rest(url, value)
		wait.Done()
	}(ip)

	wait.Wait()
	return err
}

func (c *HTTPClient) AllWorkerBlockBuilderPut(shard types.Shard, value []byte) error {
	var wait sync.WaitGroup
	var err error

	ip := "http://127.0.0.1:8000"

	wait.Add(1)
	go func(ip string) {
		url := ip + "/"
		err = c.rest(url, value)
		wait.Done()
	}(ip)

	wait.Wait()
	return err
}

// Consensus collects /history/key from every node and compare their values
func (c *HTTPClient) Consensus(k db.Key) bool {
	h := make(map[types.NodeID][]db.Value)
	for id, url := range c.HTTP {
		h[id] = make([]db.Value, 0)
		r, err := c.Client.Get(url + "/history?key=" + strconv.Itoa(int(k)))
		defer r.Body.Close()
		if err != nil {
			log.Error(err)
			continue
		}
		b, err := io.ReadAll(r.Body)
		if err != nil {
			log.Error(err)
			continue
		}
		holder := h[id]
		err = json.Unmarshal(b, &holder)
		if err != nil {
			log.Error(err)
			continue
		}
		h[id] = holder
		log.Debugf("node=%v key=%v h=%v", id, k, holder)
	}
	n := 0
	for _, v := range h {
		if len(v) > n {
			n = len(v)
		}
	}
	for i := 0; i < n; i++ {
		set := make(map[string]struct{})
		for id := range c.HTTP {
			if len(h[id]) > i {
				set[string(h[id][i])] = struct{}{}
			}
		}
		if len(set) > 1 {
			return false
		}
	}
	return true
}

// Crash stops the node for t seconds then recover
// node crash forever if t < 0
func (c *HTTPClient) Crash(id types.NodeID, t int) {
	url := c.HTTP[id] + "/crash?t=" + strconv.Itoa(t)
	r, err := c.Client.Get(url)
	if err != nil {
		log.Error(err)
		return
	}
	r.Body.Close()
}

// Drop drops every message send for t seconds
func (c *HTTPClient) Drop(from, to types.NodeID, t int) {
	url := c.HTTP[from] + "/drop?id=" + string(to) + "&t=" + strconv.Itoa(t)
	r, err := c.Client.Get(url)
	if err != nil {
		log.Error(err)
		return
	}
	r.Body.Close()
}
