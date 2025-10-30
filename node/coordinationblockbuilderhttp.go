package node

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"

	"unishard/log"
	"unishard/message"
)

// serve serves the http REST API request from clients
func (bb *coordblockbuilder) http() {
	mux := http.NewServeMux()
	mux.HandleFunc("/", bb.handleRoot)
	mux.HandleFunc("/query", bb.handleQuery)
	mux.HandleFunc("/requestLeader", bb.handleRequestLeader)
	mux.HandleFunc("/reportByzantine", bb.handleReportByzantine)
	mux.HandleFunc("/WorkerBuilderRegister", bb.handleWorkerBuilderRegister)
	mux.HandleFunc("/WorkerBuilderListRequest", bb.handleWorkerBuilderListRequest)

	// http string should be in form of ":8080"
	ip, err := url.Parse("http://127.0.0.1:7999")
	if err != nil {
		log.Fatal("http url parse error: ", err)
	}
	port := ":" + ip.Port()
	bb.server = &http.Server{
		Addr:    port,
		Handler: mux,
	}
	log.CInfo("blockbuildeer http server starting on ", ip)
	log.Fatal(bb.server.ListenAndServe())
}

func (bb *coordblockbuilder) handleQuery(w http.ResponseWriter, r *http.Request) {
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

func (bb *coordblockbuilder) handleRequestLeader(w http.ResponseWriter, r *http.Request) {
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
func (bb *coordblockbuilder) handleRoot(w http.ResponseWriter, r *http.Request) {
	var req message.Transaction
	defer r.Body.Close()

	log.CDebugf("payload is %v", req)

	bb.MessageChan <- req
}

func (bb *coordblockbuilder) handleReportByzantine(w http.ResponseWriter, r *http.Request) {
	// log.CDebugf("[%v] received Handle Report Byzantine", n.id)
	// ReportByzantine message 생성
	var req message.ReportByzantine
	// n.TxChan 으로 전송
	bb.TxChan <- req
}

func (bb *coordblockbuilder) handleWorkerBuilderRegister(w http.ResponseWriter, r *http.Request) {
	var req message.WorkerBuilderRegister
	v, _ := io.ReadAll(r.Body)
	err := json.Unmarshal(v, &req)
	if err != nil {
		log.CDebugf("error occurred when receiving WorkerBuilderRegister")
	}
	defer r.Body.Close()
	bb.MessageChan <- req
}

func (bb *coordblockbuilder) handleWorkerBuilderListRequest(w http.ResponseWriter, r *http.Request) {
	var req message.WorkerBuilderListRequest
	defer r.Body.Close()
	req.Gateway = r.URL.Query().Get("Gateway")
	bb.MessageChan <- req
}
