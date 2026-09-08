package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
)

type RPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  []int  `json:"params"`
	ID      int    `json:"id"`
}

type RPCResponse struct {
	JSONRPC string `json:"jsonrpc"`
	Result  any    `json:"result,omitempty"`
	Error   any    `json:"error,omitempty"`
	ID      int    `json:"id"`
}

func rpcHandler(w http.ResponseWriter, r *http.Request) {

	var req RPCRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	res := RPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
	}

	switch req.Method {

	case "add":
		if len(req.Params) != 2 {
			res.Error = "need 2 numbers"
		} else {
			res.Result = req.Params[0] + req.Params[1]
		}

	case "multiply":
		if len(req.Params) != 2 {
			res.Error = "need 2 numbers"
		} else {
			res.Result = req.Params[0] * req.Params[1]
		}

	default:
		res.Error = "method not found"
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(res)
}

func main() {

	http.HandleFunc("/rpc", rpcHandler)

	fmt.Println("RPC Server : http://localhost:8080/rpc")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
