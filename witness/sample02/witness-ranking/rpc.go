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

/*
 * Witness一覧のRPCレスポンス
 */
type rpcResponse struct {
	Result []struct {
		Owner                 string      `json:"owner"`
		Votes                 json.Number `json:"votes"`
		RunningVersion        string      `json:"running_version"`
		SigningKey            string      `json:"signing_key"`
		TotalMissed           int64       `json:"total_missed"`
		LastSBDExchangeUpdate string      `json:"last_sbd_exchange_update"`
		LastASlot             uint64      `json:"last_aslot"`
	} `json:"result"`
}

/*
 * Dynamic Global Properties のRPCレスポンス
 */
type dynamicGlobalPropertiesResponse struct {
	Result struct {
		Time         string `json:"time"`
		CurrentASlot uint64 `json:"current_aslot"`
	} `json:"result"`
}

/*
 * Witness一覧を取得
 */
func FetchWitnesses(rpcURL string) ([]Witness, error) {

	/*
	 * 現在のBlockchain時刻と
	 * current_aslotを取得
	 */
	currentTime, currentASlot, err :=
		FetchDynamicGlobalProperties(rpcURL)

	if err != nil {
		return nil, err
	}

	/*
	 * Witness一覧を取得
	 */
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

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := client.Post(
		rpcURL,
		"application/json",
		bytes.NewReader(body),
	)

	if err != nil {
		return nil, fmt.Errorf("RPC request: %w", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)

		return nil, fmt.Errorf(
			"RPC status %s: %s",
			resp.Status,
			string(b),
		)
	}

	var result rpcResponse

	dec := json.NewDecoder(resp.Body)
	dec.UseNumber()

	if err := dec.Decode(&result); err != nil {
		return nil, fmt.Errorf(
			"decode RPC response: %w",
			err,
		)
	}

	witnesses := make(
		[]Witness,
		0,
		len(result.Result),
	)

	for _, w := range result.Result {

		/*
		 * Witnessが最後にブロックを
		 * 生成した時刻
		 */
		lastBlockTime := ""

		if w.LastASlot > 0 &&
			currentASlot >= w.LastASlot {

			/*
			 * 現在slotとの差
			 */
			slotDiff :=
				currentASlot - w.LastASlot

			/*
			 * Steemは1slot = 3秒
			 */
			elapsedSeconds :=
				slotDiff * 3

			/*
			 * 現在のBlockchain時刻から
			 * 経過秒数を引く
			 */
			lastTime := currentTime.Add(
				-time.Duration(elapsedSeconds) *
					time.Second,
			)

			/*
			 * UTC形式で保存
			 */
			lastBlockTime = lastTime.UTC().Format(
				"2006-01-02T15:04:05",
			)
		}

		witnesses = append(
			witnesses,
			Witness{
				Name:           w.Owner,
				Votes:          string(w.Votes),
				RunningVersion: w.RunningVersion,
				SigningKey:     w.SigningKey,
				TotalMissed:    w.TotalMissed,

				/*
				 * 価格更新時刻
				 */
				LastUpdate: w.LastSBDExchangeUpdate,

				/*
				 * 最後のブロック生成時刻
				 */
				LastBlockTime: lastBlockTime,
			},
		)
	}

	return witnesses, nil
}

/*
 * 現在のBlockchain状態を取得
 *
 * 戻り値:
 *
 * 現在のBlockchain時刻
 * current_aslot
 */
func FetchDynamicGlobalProperties(
	rpcURL string,
) (
	time.Time,
	uint64,
	error,
) {

	reqBody := rpcRequest{
		JSONRPC: "2.0",
		Method:  "condenser_api.get_dynamic_global_properties",
		Params:  []interface{}{},
		ID:      1,
	}

	body, err := json.Marshal(reqBody)

	if err != nil {
		return time.Time{}, 0, err
	}

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := client.Post(
		rpcURL,
		"application/json",
		bytes.NewReader(body),
	)

	if err != nil {
		return time.Time{}, 0,
			fmt.Errorf(
				"dynamic properties RPC request: %w",
				err,
			)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)

		return time.Time{}, 0,
			fmt.Errorf(
				"dynamic properties RPC status %s: %s",
				resp.Status,
				string(b),
			)
	}

	var result dynamicGlobalPropertiesResponse

	if err := json.NewDecoder(resp.Body).Decode(
		&result,
	); err != nil {

		return time.Time{}, 0,
			fmt.Errorf(
				"decode dynamic properties: %w",
				err,
			)
	}

	/*
	 * Blockchainの時刻はUTC
	 */
	currentTime, err := time.Parse(
		"2006-01-02T15:04:05",
		result.Result.Time,
	)

	if err != nil {
		return time.Time{}, 0,
			fmt.Errorf(
				"parse blockchain time %s: %w",
				result.Result.Time,
				err,
			)
	}

	return currentTime,
		result.Result.CurrentASlot,
		nil
}
