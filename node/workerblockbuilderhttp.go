package node

import (
	"io"
	"net/http"
	"net/url"
	"strconv"
	"unishard/log"
	"unishard/message"
)

// serve serves the http REST API request from clients
func (bb *blockbuilder) http() {
	mux := http.NewServeMux()
	mux.HandleFunc("/", bb.handleRoot)
	mux.HandleFunc("/query", bb.handleQuery)
	mux.HandleFunc("/requestLeader", bb.handleRequestLeader)
	mux.HandleFunc("/reportByzantine", bb.handleReportByzantine)
	mux.HandleFunc("/WorkerSharedVariableRegisterRequest", bb.handleWorkerSharedVariableRegisterRequest)

	// http string should be in form of ":8080"
	shardPort := 8000 + int(bb.shard)
	ip, err := url.Parse("http://127.0.0.1:" + strconv.Itoa(shardPort))
	if err != nil {
		log.Fatal("http url parse error: ", err)
	}
	port := ":" + ip.Port()
	bb.server = &http.Server{
		Addr:    port,
		Handler: mux,
	}
	log.Infof("Shard [%v] blockbuildeer http server starting on %v", bb.shard, ip)
	log.Fatal(bb.server.ListenAndServe())
}

func (bb *blockbuilder) handleQuery(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var query message.Query
	query.C = make(chan message.QueryReply)
	bb.TxChan <- query
	reply := <-query.C
	_, err := io.WriteString(w, reply.Info)
	if err != nil {
		log.Error(err)
	}
}

func (bb *blockbuilder) handleRequestLeader(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var reqLeader message.RequestLeader
	reqLeader.C = make(chan message.RequestLeaderReply)
	bb.TxChan <- reqLeader
	reply := <-reqLeader.C
	_, err := io.WriteString(w, reply.Leader)
	if err != nil {
		log.Error(err)
	}
}

// transaction 받는 부분
func (bb *blockbuilder) handleRoot(w http.ResponseWriter, r *http.Request) {
	var req message.Transaction
	defer r.Body.Close()

	bb.MessageChan <- req
}

func (bb *blockbuilder) handleReportByzantine(w http.ResponseWriter, r *http.Request) {
	// log.Debugf("[%v] received Handle Report Byzantine", n.id)
	// ReportByzantine message 생성
	var req message.ReportByzantine
	// n.TxChan 으로 전송
	bb.TxChan <- req
}

func (bb *blockbuilder) handleWorkerSharedVariableRegisterRequest(w http.ResponseWriter, r *http.Request) {
	var req message.WorkerSharedVariableRegisterRequest
	defer r.Body.Close()
	req.Gateway = r.URL.Query().Get("Gateway")
	bb.MessageChan <- req
}
