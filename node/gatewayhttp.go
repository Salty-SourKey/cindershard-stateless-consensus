package node

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"unishard/log"
	"unishard/message"
	"unishard/types"
)

// serve serves the http REST API request from clients
func (gn *gatewaynode) http() {
	mux := http.NewServeMux()
	mux.HandleFunc("/", gn.handleRoot)
	mux.HandleFunc("/WorkerSharedVariableRegisterResponse", gn.handleWorkerSharedVariableRegisterResponse)
	mux.HandleFunc("/WorkerBuilderListResponse", gn.handleWorkerBuilderListResponse)

	// http string should be in form of ":8080"
	ip, err := url.Parse("http://127.0.0.1:8000")
	if err != nil {
		log.Fatal("http url parse error: ", err)
	}
	port := ":" + ip.Port()
	gn.server = &http.Server{
		Addr:    port,
		Handler: mux,
	}
	log.Info("Gateway http server starting on ", port)
	log.Fatal(gn.server.ListenAndServe())
}

// transaction 받는 부분
func (gn *gatewaynode) handleRoot(w http.ResponseWriter, r *http.Request) {
	var req message.TransactionForm
	defer r.Body.Close()
	v, _ := io.ReadAll(r.Body)
	if err := json.Unmarshal(v, &req); err != nil {
		log.Errorf("error occurred when receiving transaction")
	}

	gn.MessageChan <- req
}

func (gn *gatewaynode) handleWorkerSharedVariableRegisterResponse(w http.ResponseWriter, r *http.Request) {
	var req message.WorkerSharedVariableRegisterResponse
	defer r.Body.Close()
	senderShard, _ := strconv.Atoi(r.URL.Query().Get("SenderShard"))
	variables := r.URL.Query().Get("Variables")
	req.SenderShard = types.Shard(senderShard)
	req.Variables = variables
	gn.MessageChan <- req
}
func (gn *gatewaynode) handleWorkerBuilderListResponse(w http.ResponseWriter, r *http.Request) {
	var req message.WorkerBuilderListResponse
	defer r.Body.Close()
	v, _ := io.ReadAll(r.Body)
	err := json.Unmarshal(v, &req)
	if err != nil {
		log.CDebugf("error occurred when receiving WorkerBuilderListResponse")
	}
	gn.MessageChan <- req
}
