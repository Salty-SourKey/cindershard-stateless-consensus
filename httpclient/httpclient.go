package httpclient

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httputil"
	"strconv"
	"unishard/config"
	"unishard/log"
	"unishard/node"
	"unishard/types"
	"unishard/utils"
)

type Client interface {
	GetWorkerBlockBuilder(shardNum types.Shard, pattern string, value string) error
	PutWorkerBlockBuilder(shardNum types.Shard, pattern string, value []byte) error
	GetCoordinationBlockBuilder(pattern string, value string) error
	PutCoordinationBlockBuilder(pattern string, value []byte) error
	GetGateway(pattern string, value string) error
	PutGateway(pattern string, value []byte) error
}

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

func (c *HTTPClient) GetWorkerBlockBuilder(shardNum types.Shard, pattern string, value string) error {
	c.CID++
	log.Debugf("method GetWorkerBlockBuilder %s, %s, %x", pattern, value, shardNum)
	url := c.HTTP[utils.NewNodeID(int(shardNum))] + pattern + value
	return c.rest(url, nil)
}

func (c *HTTPClient) PutWorkerBlockBuilder(shardNum types.Shard, pattern string, value []byte) error {
	c.CID++
	log.Debugf("method PutWorkerBlockBuilder %s, %s", pattern, value)

	url := c.HTTP[utils.NewNodeID(int(shardNum))]

	return c.rest(url, value)
}

func (c *HTTPClient) GetCoordinationBlockBuilder(pattern string, value string) error {
	c.CID++
	log.Debugf("method GetCoordinationBlockBuilder %s, %s", pattern, value)

	url := c.HTTP[utils.NewNodeID(0)] + pattern + value
	return c.rest(url, nil)
}

func (c *HTTPClient) PutCoordinationBlockBuilder(pattern string, value []byte) error {
	c.CID++
	log.Debugf("method PutCoordinationBlockBuilder %s, %s", pattern, value)

	url := c.HTTP[utils.NewNodeID(0)] + pattern
	return c.rest(url, value)
}

func (c *HTTPClient) GetGateway(pattern string, value string) error {
	c.CID++
	log.Debugf("method GetGateway %s, %s", pattern, value)
	gatewayShard := config.GetConfig().ShardCount + 1

	url := c.HTTP[utils.NewNodeID(gatewayShard)] + pattern + value
	return c.rest(url, nil)
}

func (c *HTTPClient) PutGateway(pattern string, value []byte) error {
	c.CID++
	log.Debugf("method PutGateway %s, %s", pattern, value)
	gatewayShard := config.GetConfig().ShardCount + 1

	url := c.HTTP[utils.NewNodeID(gatewayShard)] + pattern
	return c.rest(url, value)
}

func (c *HTTPClient) rest(url string, value []byte) error {
	method := http.MethodGet
	var body io.Reader
	if value != nil {
		method = http.MethodPut
		body = bytes.NewBuffer(value)
	}
	//v, _ := io.ReadAll(body)
	log.Debugf("method %s, %s, %s", method, url, body)
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
