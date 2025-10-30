package node

import (
	"io"
	"net/http"
	"net/url"
	"strconv"

	"unishard/log"
	"unishard/message"
	"unishard/utils"
)

// http request header names
const (
	HTTPClientID  = "Id"
	HTTPCommandID = "Cid"
)

// serve serves the http REST API request from clients
func (n *node) http() {
	mux := http.NewServeMux()
	mux.HandleFunc("/", n.handleRoot)
	mux.HandleFunc("/query", n.handleQuery)
	mux.HandleFunc("/requestLeader", n.handleRequestLeader)
	mux.HandleFunc("/reportByzantine", n.handleReportByzantine)

	ip, err := url.Parse("http://127.0.0.1:" + strconv.Itoa(9000+utils.Node(n.ID())+int(n.shard)*100))
	if err != nil {
		log.Fatal("http url parse error: ", err)
	}
	port := ":" + ip.Port()
	n.server = &http.Server{
		Addr:    port,
		Handler: mux,
	}
	log.Info("http server starting on ", port)
	log.Fatal(n.server.ListenAndServe())
}

func (n *node) handleQuery(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var query message.Query
	query.C = make(chan message.QueryReply)
	n.TxChan <- query
	reply := <-query.C
	_, err := io.WriteString(w, reply.Info)
	if err != nil {
		log.Error(err)
	}
}

func (n *node) handleRequestLeader(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var reqLeader message.RequestLeader
	reqLeader.C = make(chan message.RequestLeaderReply)
	n.TxChan <- reqLeader
	reply := <-reqLeader.C
	_, err := io.WriteString(w, reply.Leader)
	if err != nil {
		log.Error(err)
	}
}

// transaction 받는 부분
func (n *node) handleRoot(w http.ResponseWriter, r *http.Request) {
	var req message.Transaction
	defer r.Body.Close()

	v, _ := io.ReadAll(r.Body)
	log.Debugf("[%v] payload is %x", n.id, v)
	n.TxChan <- req
}

func (n *node) handleReportByzantine(w http.ResponseWriter, r *http.Request) {
	// log.Debugf("[%v] received Handle Report Byzantine", n.id)
	// ReportByzantine message 생성
	var req message.ReportByzantine
	// n.TxChan 으로 전송
	n.TxChan <- req
}
