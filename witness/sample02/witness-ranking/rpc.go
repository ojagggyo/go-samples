package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type rpcRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params"`
	ID      int         `json:"id"`
}

type rpcResponse struct {
	Result []struct {
		Owner                 string      `json:"owner"`
		Votes                 json.Number `json:"votes"`
		RunningVersion        string      `json:"running_version"`
		SigningKey            string      `json:"signing_key"`
		TotalMissed           int64       `json:"total_missed"`
		LastSBDExchangeUpdate string      `json:"last_sbd_exchange_update"`
	} `json:"result"`
}

func FetchWitnesses(rpcURL string) ([]Witness, error) {
	reqBody := rpcRequest{
		JSONRPC: "2.0",
		Method:  "condenser_api.get_witnesses_by_vote",
		Params:  []interface{}{nil, 300},
		ID:      1,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Post(rpcURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("RPC request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("RPC status %s: %s", resp.Status, string(b))
	}

	var result rpcResponse
	dec := json.NewDecoder(resp.Body)
	dec.UseNumber()

	if err := dec.Decode(&result); err != nil {
		return nil, fmt.Errorf("decode RPC response: %w", err)
	}

	witnesses := make([]Witness, 0, len(result.Result))

	for _, w := range result.Result {
		witnesses = append(witnesses, Witness{
			Name:           w.Owner,
			Votes:          string(w.Votes),
			RunningVersion: w.RunningVersion,
			SigningKey:     w.SigningKey,
			TotalMissed:    w.TotalMissed,
			LastUpdate:     w.LastSBDExchangeUpdate,
		})
	}

	return witnesses, nil
}
