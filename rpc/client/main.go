package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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
	Result  int    `json:"result"`
	Error   any    `json:"error"`
	ID      int    `json:"id"`
}

func call(method string, a, b int) {

	req := RPCRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  []int{a, b},
		ID:      1,
	}

	body, _ := json.Marshal(req)

	resp, err := http.Post(
		"http://localhost:8080/rpc",
		"application/json",
		bytes.NewBuffer(body),
	)

	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)

	var res RPCResponse
	json.Unmarshal(data, &res)

	fmt.Printf("%s(%d,%d) = %d\n", method, a, b, res.Result)
}

func main() {

	call("add", 10, 20)

	call("multiply", 5, 8)
}
