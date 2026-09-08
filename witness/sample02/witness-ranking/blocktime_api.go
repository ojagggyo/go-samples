package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

const (
	/*
	 * 1回のJSON-RPCで取得する
	 * ブロック数
	 */
	blockTimeBatchSize = 100

	/*
	 * 最新ブロックから
	 * 最大何ブロックまで調べるか
	 */
	blockTimeMaxBlocks = 1000

	/*
	 * 1回のAPIで取得できる
	 * Witnessの最大数
	 *
	 * ranking.js
	 *
	 * 1～20位     → 20件
	 * 21～100位   → 80件
	 */
	blockTimeMaxUsers = 100
)

type blockTimeRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params"`
	ID      int         `json:"id"`
}

type blockTimeDynamicGlobalPropertiesResponse struct {
	Result struct {
		HeadBlockNumber uint32 `json:"head_block_number"`
		Time            string `json:"time"`
	} `json:"result"`

	Error interface{} `json:"error"`
}

type blockTimeBlockResponse struct {
	ID int `json:"id"`

	Result *struct {
		Timestamp string `json:"timestamp"`
		Witness   string `json:"witness"`
	} `json:"result"`

	Error interface{} `json:"error"`
}

type blockTimeResult struct {
	Name      string `json:"name"`
	BlockTime string `json:"block_time"`
}

func init() {
	http.HandleFunc(
		"/api/block-times",
		handleBlockTimes,
	)
}

/*
 * 指定されたWitnessの
 * 最新ブロック生成時刻を取得
 *
 * 例：
 *
 * /api/block-times?users=justyy,steemchiller,...
 *
 * 最大100人まで
 */
func handleBlockTimes(
	w http.ResponseWriter,
	r *http.Request,
) {

	w.Header().Set(
		"Content-Type",
		"application/json; charset=utf-8",
	)

	w.Header().Set(
		"Cache-Control",
		"no-store, no-cache, must-revalidate",
	)

	usersParam := r.URL.Query().Get(
		"users",
	)

	if usersParam == "" {

		http.Error(
			w,
			`{"error":"users is required"}`,
			http.StatusBadRequest,
		)

		return
	}

	/*
	 * カンマ区切りのWitness名を取得
	 */
	names := strings.Split(
		usersParam,
		",",
	)

	/*
	 * 最大100人まで
	 *
	 * 以前はここが20人になっていたため、
	 * 21～100位の80人を送っても
	 * 最初の20人だけになっていた。
	 */
	if len(names) > blockTimeMaxUsers {

		names = names[:blockTimeMaxUsers]

	}

	/*
	 * 検索対象
	 */
	targets := make(
		map[string]bool,
	)

	for _, name := range names {

		name = strings.TrimSpace(
			name,
		)

		if name != "" {

			targets[name] = true

		}

	}

	if len(targets) == 0 {

		http.Error(
			w,
			`{"error":"no valid users"}`,
			http.StatusBadRequest,
		)

		return
	}

	/*
	 * Steem RPC
	 */
	rpcURL := strings.TrimSpace(
		os.Getenv("STEEM_RPC_URL"),
	)

	if rpcURL == "" {
		http.Error(
			w,
			`{"error":"STEEM_RPC_URL is not set"}`,
			http.StatusInternalServerError,
		)
		return
	}

	rpcURL = strings.TrimRight(rpcURL, "/")

	/*
	 * 現在のHead Block番号を取得
	 */
	headBlockNumber, err :=
		fetchBlockTimeHeadBlockNumber(
			rpcURL,
		)

	if err != nil {

		http.Error(
			w,
			fmt.Sprintf(
				`{"error":%q}`,
				err.Error(),
			),
			http.StatusBadGateway,
		)

		return
	}

	/*
	 * 各Witnessの
	 * 最新ブロックを検索
	 */
	results, err := findLastBlocks(
		rpcURL,
		headBlockNumber,
		targets,
	)

	if err != nil {

		http.Error(
			w,
			fmt.Sprintf(
				`{"error":%q}`,
				err.Error(),
			),
			http.StatusBadGateway,
		)

		return
	}

	/*
	 * リクエストされた順番で返す
	 */
	ordered := make(
		[]blockTimeResult,
		0,
		len(names),
	)

	for _, name := range names {

		name = strings.TrimSpace(
			name,
		)

		if result, ok :=
			results[name]; ok {

			ordered = append(
				ordered,
				result,
			)

		}

	}

	/*
	 * JSONで返す
	 */
	if err := json.NewEncoder(
		w,
	).Encode(
		ordered,
	); err != nil {

		fmt.Println(
			"encode block time response:",
			err,
		)

	}

}

/*
 * 最新Head Block番号を取得
 */
func fetchBlockTimeHeadBlockNumber(
	rpcURL string,
) (
	uint32,
	error,
) {

	request := blockTimeRequest{
		JSONRPC: "2.0",

		Method: "condenser_api.get_dynamic_global_properties",

		Params: []interface{}{},

		ID: 1,
	}

	body, err := json.Marshal(
		request,
	)

	if err != nil {

		return 0, err

	}

	client := &http.Client{
		Timeout: 15 * time.Second,
	}

	resp, err := client.Post(
		rpcURL,
		"application/json",
		bytes.NewReader(
			body,
		),
	)

	if err != nil {

		return 0, fmt.Errorf(
			"dynamic properties RPC: %w",
			err,
		)

	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {

		data, _ :=
			io.ReadAll(
				resp.Body,
			)

		return 0, fmt.Errorf(
			"dynamic properties RPC status %s: %s",
			resp.Status,
			string(data),
		)

	}

	var response blockTimeDynamicGlobalPropertiesResponse

	if err := json.NewDecoder(
		resp.Body,
	).Decode(
		&response,
	); err != nil {

		return 0, fmt.Errorf(
			"decode dynamic properties: %w",
			err,
		)

	}

	if response.Error != nil {

		return 0, fmt.Errorf(
			"dynamic properties RPC error: %v",
			response.Error,
		)

	}

	if response.Result.HeadBlockNumber == 0 {

		return 0, fmt.Errorf(
			"head block number is zero",
		)

	}

	return response.Result.HeadBlockNumber, nil
}

/*
 * 指定Witnessの
 * 最新ブロックを検索
 *
 * 最新ブロックから過去へ向かって
 * 最大1000ブロック調査する。
 */
func findLastBlocks(
	rpcURL string,
	headBlockNumber uint32,
	targets map[string]bool,
) (
	map[string]blockTimeResult,
	error,
) {

	found := make(
		map[string]blockTimeResult,
	)

	remaining := make(
		map[string]bool,
	)

	/*
	 * まだ見つかっていないWitness
	 */
	for name := range targets {

		remaining[name] = true

	}

	var scanned uint32

	blockNum := headBlockNumber

	for blockNum > 0 {

		/*
		 * 全Witnessが見つかった
		 */
		if len(remaining) == 0 {

			break

		}

		/*
		 * 最大検索ブロック数に到達
		 */
		if scanned >= blockTimeMaxBlocks {

			break

		}

		/*
		 * 今回取得するブロック数
		 */
		count :=
			blockTimeBatchSize

		remainingScan :=
			blockTimeMaxBlocks -
				int(scanned)

		if count > remainingScan {

			count = remainingScan

		}

		if uint32(count) > blockNum {

			count = int(blockNum)

		}

		if count <= 0 {

			break

		}

		/*
		 * JSON-RPC Batchで
		 * 複数ブロックをまとめて取得
		 */
		responses, err := fetchBlockBatch(
			rpcURL,
			blockNum,
			count,
		)

		if err != nil {

			return nil, err

		}

		/*
		 * JSON-RPCのバッチ応答順は
		 * 保証されないためID順に並べる。
		 *
		 * ID 1 = 最新ブロック
		 * ID 2 = 1つ前のブロック
		 */
		sort.Slice(
			responses,

			func(
				i,
				j int,
			) bool {

				return responses[i].ID <
					responses[j].ID

			},
		)

		/*
		 * 最新ブロックから順番に調べる。
		 *
		 * 最初に見つかったブロックが
		 * そのWitnessの最新ブロック。
		 */
		for _, response := range responses {

			if response.Error != nil {

				continue

			}

			if response.Result == nil {

				continue

			}

			witness :=
				response.Result.Witness

			if !remaining[witness] {

				continue

			}

			if response.Result.Timestamp == "" {

				continue

			}

			found[witness] =
				blockTimeResult{
					Name: witness,

					BlockTime: response.Result.Timestamp,
				}

			delete(
				remaining,
				witness,
			)

		}

		scanned +=
			uint32(count)

		if blockNum <= uint32(count) {

			break

		}

		blockNum -=
			uint32(count)

	}

	return found, nil
}

/*
 * 指定ブロックから
 * count個のブロックを
 * JSON-RPC Batchで取得
 */
func fetchBlockBatch(
	rpcURL string,
	startBlock uint32,
	count int,
) (
	[]blockTimeBlockResponse,
	error,
) {

	requests := make(
		[]blockTimeRequest,
		0,
		count,
	)

	for i := 0; i < count; i++ {

		blockNum :=
			startBlock -
				uint32(i)

		requests = append(
			requests,

			blockTimeRequest{
				JSONRPC: "2.0",

				Method: "condenser_api.get_block",

				Params: []interface{}{
					blockNum,
				},

				ID: i + 1,
			},
		)

	}

	body, err := json.Marshal(
		requests,
	)

	if err != nil {

		return nil, err

	}

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := client.Post(
		rpcURL,
		"application/json",
		bytes.NewReader(
			body,
		),
	)

	if err != nil {

		return nil, fmt.Errorf(
			"block batch RPC: %w",
			err,
		)

	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {

		data, _ :=
			io.ReadAll(
				resp.Body,
			)

		return nil, fmt.Errorf(
			"block batch RPC status %s: %s",
			resp.Status,
			string(data),
		)

	}

	var responses []blockTimeBlockResponse

	if err := json.NewDecoder(
		resp.Body,
	).Decode(
		&responses,
	); err != nil {

		return nil, fmt.Errorf(
			"decode block batch: %w",
			err,
		)

	}

	return responses, nil
}
