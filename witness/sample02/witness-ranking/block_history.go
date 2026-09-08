package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

type witnessBlock struct {
	Time        string `json:"time"`
	BlockNum    uint32 `json:"block_num"`
	BeforeRange bool   `json:"before_range,omitempty"`
}

type steemRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params"`
	ID      int         `json:"id"`
}

type steemRPCEnvelope struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type dynamicGlobalProperties struct {
	HeadBlockNumber uint32 `json:"head_block_number"`
	Time            string `json:"time"`
}

type blockHeader struct {
	Header struct {
		Timestamp string `json:"timestamp"`
	} `json:"header"`
}

type enumVirtualOpsResult struct {
	Ops                 []steemVirtualOp `json:"ops"`
	NextBlockRangeBegin uint32           `json:"next_block_range_begin"`
}

type steemVirtualOp struct {
	Block     uint32 `json:"block"`
	Timestamp string `json:"timestamp"`
	Op        struct {
		Type  string          `json:"type"`
		Value json.RawMessage `json:"value"`
	} `json:"op"`
}

func steemRPCURL() (string, error) {
	v := strings.TrimSpace(os.Getenv("STEEM_RPC_URL"))
	if v == "" {
		return "", fmt.Errorf("STEEM_RPC_URL is not set")
	}
	return strings.TrimRight(v, "/"), nil
}

func steemRPC(ctx context.Context, method string, params interface{}, out interface{}) error {
	payload, err := json.Marshal(steemRPCRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
		ID:      1,
	})
	if err != nil {
		return err
	}

	apiURL, err := steemRPCURL()
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		apiURL,
		bytes.NewReader(payload),
	)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Steem RPC HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var envelope steemRPCEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return err
	}
	if envelope.Error != nil {
		return fmt.Errorf("Steem RPC error: %s", envelope.Error.Message)
	}
	if len(envelope.Result) == 0 || string(envelope.Result) == "null" {
		return fmt.Errorf("Steem RPC returned no result for %s", method)
	}
	return json.Unmarshal(envelope.Result, out)
}

func parseSteemTime(s string) (time.Time, error) {
	if strings.TrimSpace(s) == "" {
		return time.Time{}, fmt.Errorf("empty Steem timestamp")
	}
	return time.ParseInLocation("2006-01-02T15:04:05", s, time.UTC)
}

// getBlockNumberAtOrBefore finds a block whose timestamp is at or before target.
// Block timestamps are monotonic, so a binary search avoids scanning the whole chain.
func getBlockNumberAtOrBefore(ctx context.Context, target time.Time, head uint32) (uint32, error) {
	var props dynamicGlobalProperties
	if err := steemRPC(ctx, "database_api.get_dynamic_global_properties", []interface{}{}, &props); err != nil {
		return 0, err
	}
	if head == 0 || head > props.HeadBlockNumber {
		head = props.HeadBlockNumber
	}

	var headHeader blockHeader
	if err := steemRPC(ctx, "block_api.get_block_header", map[string]interface{}{"block_num": head}, &headHeader); err != nil {
		return 0, err
	}
	headTime, err := parseSteemTime(headHeader.Header.Timestamp)
	if err != nil {
		return 0, err
	}
	if target.After(headTime) {
		return head, nil
	}

	// The chain targets one block every 3 seconds. Use that only as an initial bound;
	// binary search against actual headers handles missed slots correctly.
	deltaSeconds := int64(headTime.Sub(target).Seconds())
	var high = head
	var low uint32
	if deltaSeconds > 0 {
		estimate := int64(head) - deltaSeconds/3 - 1000
		if estimate > 1 {
			low = uint32(estimate)
		}
	}

	// If the initial bound is too recent, move it back until it is at/before target.
	for low > 1 {
		var h blockHeader
		if err := steemRPC(ctx, "block_api.get_block_header", map[string]interface{}{"block_num": low}, &h); err != nil {
			return 0, err
		}
		t, err := parseSteemTime(h.Header.Timestamp)
		if err != nil {
			return 0, err
		}
		if !t.After(target) {
			break
		}
		span := high - low
		if span == 0 {
			low = 1
			break
		}
		if span > 10000 {
			low -= span / 2
		} else {
			low = low / 2
		}
	}

	for low+1 < high {
		mid := low + (high-low)/2
		var h blockHeader
		if err := steemRPC(ctx, "block_api.get_block_header", map[string]interface{}{"block_num": mid}, &h); err != nil {
			return 0, err
		}
		t, err := parseSteemTime(h.Header.Timestamp)
		if err != nil {
			return 0, err
		}
		if t.After(target) {
			high = mid
		} else {
			low = mid
		}
	}
	return low, nil
}

func getWitnessBlocks(ctx context.Context, user string, from, to time.Time) ([]witnessBlock, error) {
	if from.After(to) {
		return []witnessBlock{}, nil
	}

	var props dynamicGlobalProperties
	if err := steemRPC(ctx, "database_api.get_dynamic_global_properties", []interface{}{}, &props); err != nil {
		return nil, err
	}

	fromUTC := from.UTC()
	toUTC := to.UTC()
	scanFrom := fromUTC.Add(-24 * time.Hour)
	startBlock, err := getBlockNumberAtOrBefore(ctx, scanFrom, props.HeadBlockNumber)
	if err != nil {
		return nil, err
	}
	endBlock := props.HeadBlockNumber

	var headTime time.Time
	if t, err := parseSteemTime(props.Time); err == nil {
		headTime = t
	}
	if !headTime.IsZero() && toUTC.Before(headTime) {
		endBlock, err = getBlockNumberAtOrBefore(ctx, toUTC, props.HeadBlockNumber)
		if err != nil {
			return nil, err
		}
	}
	if endBlock < startBlock {
		return []witnessBlock{}, nil
	}

	blocks := make([]witnessBlock, 0)
	const chunkSize uint32 = 10000

	for begin := startBlock; begin <= endBlock; {
		end := begin + chunkSize
		if end < begin || end > endBlock+1 {
			end = endBlock + 1
		}

		var pageStart = begin
		for pageStart < end {
			var result enumVirtualOpsResult
			err := steemRPC(ctx, "account_history_api.enum_virtual_ops", map[string]interface{}{
				"block_range_begin": pageStart,
				"block_range_end":   end,
			}, &result)
			if err != nil {
				return nil, err
			}

			for _, op := range result.Ops {
				if op.Op.Type != "producer_reward_operation" {
					continue
				}

				var payload struct {
					Producer string `json:"producer"`
				}
				if err := json.Unmarshal(op.Op.Value, &payload); err != nil {
					continue
				}
				if payload.Producer != user {
					continue
				}

				var ts time.Time

				if strings.TrimSpace(op.Timestamp) != "" {
					ts, err = parseSteemTime(op.Timestamp)
					if err != nil {
						return nil, fmt.Errorf(
							"invalid virtual op timestamp: block=%d producer=%s timestamp=%q: %w",
							op.Block,
							payload.Producer,
							op.Timestamp,
							err,
						)
					}
				} else {
					// If the virtual operation has no timestamp,
					// obtain the timestamp from its block header.
					var header blockHeader

					if err := steemRPC(
						ctx,
						"block_api.get_block_header",
						map[string]interface{}{
							"block_num": op.Block,
						},
						&header,
					); err != nil {
						return nil, fmt.Errorf(
							"failed to get block header: block=%d: %w",
							op.Block,
							err,
						)
					}

					if strings.TrimSpace(header.Header.Timestamp) == "" {
						return nil, fmt.Errorf(
							"empty block header timestamp: block=%d",
							op.Block,
						)
					}

					ts, err = parseSteemTime(header.Header.Timestamp)
					if err != nil {
						return nil, fmt.Errorf(
							"invalid block header timestamp: block=%d timestamp=%q: %w",
							op.Block,
							header.Header.Timestamp,
							err,
						)
					}
				}

				if ts.After(toUTC) {
					continue
				}

				blocks = append(blocks, witnessBlock{
					Time:        ts.Local().Format("2006-01-02 15:04:05"),
					BlockNum:    op.Block,
					BeforeRange: ts.Before(fromUTC),
				})
			}

			if result.NextBlockRangeBegin == 0 || result.NextBlockRangeBegin <= pageStart || result.NextBlockRangeBegin >= end {
				break
			}
			pageStart = result.NextBlockRangeBegin
		}

		if end > endBlock {
			break
		}
		begin = end
	}

	sort.Slice(blocks, func(i, j int) bool {
		if blocks[i].Time == blocks[j].Time {
			return blocks[i].BlockNum < blocks[j].BlockNum
		}
		return blocks[i].Time < blocks[j].Time
	})

	// enum_virtual_ops can overlap at pagination boundaries on some node versions.
	// Remove duplicate block numbers before returning to the browser.
	unique := blocks[:0]
	seen := make(map[uint32]struct{}, len(blocks))
	for _, b := range blocks {
		if _, ok := seen[b.BlockNum]; ok {
			continue
		}
		seen[b.BlockNum] = struct{}{}
		unique = append(unique, b)
	}
	return unique, nil
}
