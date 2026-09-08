package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/steemit/steemgosdk/api"
	"github.com/steemit/steemgosdk/broadcast"
	"github.com/steemit/steemutil/protocol"
)

const Version = "1.3.0"

const (
	maxRPCURLs          = 3
	rpcFailureThreshold = 3
)

// Configured signing-key chainの最後のキーになったため監視を停止する。
var ErrMonitoringStopped = errors.New("monitoring stopped")

type Config struct {
	RPCURLs []string

	Witness          string
	ActivePrivateKey string

	// SigningKeys is an ordered failover chain.
	//
	// Example:
	//   [Primary, Backup, Failsafe]
	//   [Primary, Failsafe]
	//   [Primary, aaa, bbb, Failsafe]
	SigningKeys []string

	PollInterval  time.Duration
	StateFile     string
	VerifyTimeout time.Duration
}

type Witness struct {
	Owner       string `json:"owner"`
	URL         string `json:"url"`
	SigningKey  string `json:"signing_key"`
	TotalMissed uint64 `json:"total_missed"`

	// 監視対象Witness自身の最後に確認したブロック
	LastConfirmedBlockNum uint32 `json:"last_confirmed_block_num"`

	Props struct {
		AccountCreationFee string `json:"account_creation_fee"`
		MaximumBlockSize   uint32 `json:"maximum_block_size"`
		SBDInterestRate    uint16 `json:"sbd_interest_rate"`
	} `json:"props"`
}

type Block struct {
	Timestamp string `json:"timestamp"`
	Witness   string `json:"witness"`
}

type State struct {
	Initialized     bool   `json:"initialized"`
	LastTotalMissed uint64 `json:"last_total_missed"`

	LastFailoverAt   string `json:"last_failover_at,omitempty"`
	LastFailoverFrom string `json:"last_failover_from,omitempty"`
	LastFailoverTo   string `json:"last_failover_to,omitempty"`
}

type Controller struct {
	cfg Config

	mu sync.Mutex

	state State

	// 表示用タイムゾーン
	jst *time.Location

	// 現在使用しているRPC URLのインデックス
	currentRPCIndex int

	// 現在のRPC URLで連続して失敗した回数
	rpcFailureCount int

	api   *api.API
	bcast *broadcast.Broadcast
}

func main() {

	// ------------------------------------------------------------
	// JST
	// ------------------------------------------------------------

	jst, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		log.Fatalf(
			"failed to load JST timezone: %v",
			err,
		)
	}

	// ------------------------------------------------------------
	// --version または -v
	// ------------------------------------------------------------

	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--version", "-v":
			fmt.Printf(
				"Steem witness failover controller version %s\n",
				Version,
			)
			return
		}
	}

	cfg, err := loadConfig()
	if err != nil {
		log.Fatal(err)
	}

	log.Printf(
		"Starting Steem witness failover controller",
	)

	log.Printf(
		"Version   = %s",
		Version,
	)

	for i, url := range cfg.RPCURLs {
		log.Printf(
			"RPC[%d]    = %s",
			i+1,
			url,
		)
	}

	log.Printf(
		"Witness   = %s",
		cfg.Witness,
	)

	for i, key := range cfg.SigningKeys {
		log.Printf(
			"SigningKey[%d] = %s",
			i+1,
			key,
		)
	}

	log.Printf(
		"Interval  = %s",
		cfg.PollInterval,
	)

	state, err := loadState(cfg.StateFile)
	if err != nil {
		log.Fatal(err)
	}

	c := &Controller{
		cfg:             cfg,
		state:           state,
		jst:             jst,
		currentRPCIndex: 0,
	}

	c.setRPC(0)

	ctx, cancel := signal.NotifyContext(
		context.Background(),
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer cancel()

	if err := c.initialize(); err != nil {
		log.Fatal(err)
	}

	ticker := time.NewTicker(cfg.PollInterval)
	defer ticker.Stop()

	// 起動直後にも一度チェック。
	if err := c.check(ctx); err != nil {

		if errors.Is(
			err,
			ErrMonitoringStopped,
		) {
			log.Printf(
				"Monitoring stopped.",
			)
			return
		}

		log.Printf(
			"check error: %v",
			err,
		)
	}

	for {
		select {

		case <-ctx.Done():

			log.Printf(
				"Stopping.",
			)

			return

		case <-ticker.C:

			if err := c.check(ctx); err != nil {

				if errors.Is(
					err,
					ErrMonitoringStopped,
				) {
					log.Printf(
						"Monitoring stopped.",
					)
					return
				}

				log.Printf(
					"check error: %v",
					err,
				)
			}
		}
	}
}

// ------------------------------------------------------------
// Config
// ------------------------------------------------------------

func loadConfig() (Config, error) {

	pollSeconds := envInt(
		"POLL_SECONDS",
		2,
	)

	cfg := Config{
		Witness:          os.Getenv("STEEM_WITNESS"),
		ActivePrivateKey: os.Getenv("STEEM_ACTIVE_PRIVATE_KEY"),

		PollInterval: time.Duration(
			pollSeconds,
		) * time.Second,

		StateFile: envString(
			"STATE_FILE",
			"state.json",
		),

		VerifyTimeout: time.Duration(
			envInt(
				"VERIFY_TIMEOUT_SECONDS",
				60,
			),
		) * time.Second,
	}

	// ------------------------------------------------------------
	// RPC URLs
	// ------------------------------------------------------------

	for i := 1; i <= maxRPCURLs; i++ {

		name := fmt.Sprintf(
			"STEEM_RPC_URL_%d",
			i,
		)

		url := os.Getenv(name)

		if url != "" {
			cfg.RPCURLs = append(
				cfg.RPCURLs,
				url,
			)
		}
	}

	// 従来のSTEEM_RPC_URLとの互換性
	if len(cfg.RPCURLs) == 0 {

		url := envString(
			"STEEM_RPC_URL",
			"https://api.steemit.com",
		)

		cfg.RPCURLs = append(
			cfg.RPCURLs,
			url,
		)
	}

	if len(cfg.RPCURLs) > maxRPCURLs {
		return cfg, fmt.Errorf(
			"maximum %d RPC URLs are supported",
			maxRPCURLs,
		)
	}

	// 重複URLチェック
	for i := 0; i < len(cfg.RPCURLs); i++ {

		for j := i + 1; j < len(cfg.RPCURLs); j++ {

			if cfg.RPCURLs[i] == cfg.RPCURLs[j] {

				return cfg, fmt.Errorf(
					"duplicate RPC URL: %s",
					cfg.RPCURLs[i],
				)
			}
		}
	}

	if cfg.Witness == "" {
		return cfg, errors.New(
			"STEEM_WITNESS is required",
		)
	}

	if cfg.ActivePrivateKey == "" {
		return cfg, errors.New(
			"STEEM_ACTIVE_PRIVATE_KEY is required",
		)
	}

	// ------------------------------------------------------------
	// Signing key failover chain
	// ------------------------------------------------------------

	for i := 1; ; i++ {

		name := fmt.Sprintf(
			"STEEM_SIGNING_KEY_%d",
			i,
		)

		key := os.Getenv(name)

		if key == "" {
			break
		}

		cfg.SigningKeys = append(
			cfg.SigningKeys,
			key,
		)
	}

	// Backward compatibility
	if len(cfg.SigningKeys) == 0 {

		primary := os.Getenv(
			"STEEM_PRIMARY_SIGNING_KEY",
		)

		backup := os.Getenv(
			"STEEM_BACKUP_SIGNING_KEY",
		)

		failsafe := os.Getenv(
			"STEEM_FAILSAFE_SIGNING_KEY",
		)

		if primary != "" {
			cfg.SigningKeys = append(
				cfg.SigningKeys,
				primary,
			)
		}

		if backup != "" {
			cfg.SigningKeys = append(
				cfg.SigningKeys,
				backup,
			)
		}

		if failsafe != "" {
			cfg.SigningKeys = append(
				cfg.SigningKeys,
				failsafe,
			)
		}
	}

	if len(cfg.SigningKeys) < 2 {
		return cfg, errors.New(
			"at least two signing keys are required: STEEM_SIGNING_KEY_1, STEEM_SIGNING_KEY_2, ...",
		)
	}

	// Duplicate key check
	for i := 0; i < len(cfg.SigningKeys); i++ {

		if cfg.SigningKeys[i] == "" {

			return cfg, fmt.Errorf(
				"empty signing key at index %d",
				i+1,
			)
		}

		for j := i + 1; j < len(cfg.SigningKeys); j++ {

			if cfg.SigningKeys[i] == cfg.SigningKeys[j] {

				return cfg, fmt.Errorf(
					"duplicate signing key at positions %d and %d",
					i+1,
					j+1,
				)
			}
		}
	}

	return cfg, nil
}

// ------------------------------------------------------------
// RPC切替
// ------------------------------------------------------------

func (c *Controller) setRPC(index int) {

	if index < 0 ||
		index >= len(c.cfg.RPCURLs) {
		return
	}

	url := c.cfg.RPCURLs[index]

	c.currentRPCIndex = index
	c.rpcFailureCount = 0

	c.api = api.NewAPI(url)
	c.bcast = broadcast.NewBroadcast(url)

	log.Printf(
		"RPC endpoint selected: [%d/%d] %s",
		index+1,
		len(c.cfg.RPCURLs),
		url,
	)
}

// ------------------------------------------------------------
// RPC失敗処理
// ------------------------------------------------------------

func (c *Controller) recordRPCFailure() {

	c.rpcFailureCount++

	log.Printf(
		"RPC failure: endpoint=%d/%d consecutive_failures=%d/%d",
		c.currentRPCIndex+1,
		len(c.cfg.RPCURLs),
		c.rpcFailureCount,
		rpcFailureThreshold,
	)

	if c.rpcFailureCount < rpcFailureThreshold {
		return
	}

	if c.currentRPCIndex+1 <
		len(c.cfg.RPCURLs) {

		next := c.currentRPCIndex + 1

		log.Printf(
			"RPC endpoint failed %d consecutive times. Switching endpoint: %s -> %s",
			rpcFailureThreshold,
			c.cfg.RPCURLs[c.currentRPCIndex],
			c.cfg.RPCURLs[next],
		)

		c.setRPC(next)

		return
	}

	log.Printf(
		"All configured RPC endpoints have failed.",
	)

	c.rpcFailureCount = 0

	log.Printf(
		"RPC failure does NOT trigger failover. MISS value was not obtained.",
	)
}

func (c *Controller) recordRPCSuccess() {

	if c.rpcFailureCount > 0 {

		log.Printf(
			"RPC recovered: endpoint=%d/%d",
			c.currentRPCIndex+1,
			len(c.cfg.RPCURLs),
		)
	}

	c.rpcFailureCount = 0
}

// ------------------------------------------------------------
// Initialize
// ------------------------------------------------------------

func (c *Controller) initialize() error {

	w, err := c.getWitness()

	if err != nil {
		return err
	}

	log.Printf(
		"Current witness state: signing_key=%s total_missed=%d last_confirmed_block=%d",
		w.SigningKey,
		w.TotalMissed,
		w.LastConfirmedBlockNum,
	)

	if !c.state.Initialized {

		c.state.Initialized = true
		c.state.LastTotalMissed = w.TotalMissed

		if err := c.saveState(); err != nil {
			return err
		}

		log.Printf(
			"Initialized baseline: total_missed=%d",
			w.TotalMissed,
		)
	}

	return nil
}

// ------------------------------------------------------------
// Main check
// ------------------------------------------------------------

func (c *Controller) check(
	ctx context.Context,
) error {

	c.mu.Lock()
	defer c.mu.Unlock()

	w, err := c.getWitness()

	if err != nil {
		return err
	}

	// ------------------------------------------------------------
	// Last Generated Block / Block Age
	// ------------------------------------------------------------

	c.logLastGeneratedBlock(w)

	log.Printf(
		"witness=%s signing_key=%s total_missed=%d last=%d rpc=%s",
		c.cfg.Witness,
		w.SigningKey,
		w.TotalMissed,
		c.state.LastTotalMissed,
		c.cfg.RPCURLs[c.currentRPCIndex],
	)

	// ------------------------------------------------------------
	// 現在のSigning KeyをFailover Chainから検索
	// ------------------------------------------------------------

	currentIndex := -1

	for i, key := range c.cfg.SigningKeys {

		if w.SigningKey == key {

			currentIndex = i

			break
		}
	}

	if currentIndex == -1 {

		log.Printf(
			"CRITICAL: unexpected signing key detected: blockchain=%s",
			w.SigningKey,
		)

		return fmt.Errorf(
			"unexpected signing key: blockchain=%s",
			w.SigningKey,
		)
	}

	// ------------------------------------------------------------
	// 最後のキーなら監視停止
	// ------------------------------------------------------------

	if currentIndex ==
		len(c.cfg.SigningKeys)-1 {

		log.Printf(
			"FAILSAFE signing key is active: %s",
			w.SigningKey,
		)

		log.Printf(
			"Monitoring will be stopped. No further failover will be performed.",
		)

		return ErrMonitoringStopped
	}

	// ------------------------------------------------------------
	// MISS増加なし
	// ------------------------------------------------------------

	if w.TotalMissed <=
		c.state.LastTotalMissed {

		c.state.LastTotalMissed =
			w.TotalMissed

		return c.saveState()
	}

	// ------------------------------------------------------------
	// MISSがちょうど+1の場合だけFailover
	// ------------------------------------------------------------

	if w.TotalMissed !=
		c.state.LastTotalMissed+1 {

		log.Printf(
			"MISS increased by more than 1: %d -> %d. No failover.",
			c.state.LastTotalMissed,
			w.TotalMissed,
		)

		c.state.LastTotalMissed =
			w.TotalMissed

		return c.saveState()
	}

	targetIndex := currentIndex + 1

	targetSigningKey :=
		c.cfg.SigningKeys[targetIndex]

	log.Printf(
		"FAILOVER condition detected: total_missed %d -> %d",
		c.state.LastTotalMissed,
		w.TotalMissed,
	)

	log.Printf(
		"Failing over signing key [%d/%d] -> [%d/%d]: %s -> %s",
		currentIndex+1,
		len(c.cfg.SigningKeys),
		targetIndex+1,
		len(c.cfg.SigningKeys),
		w.SigningKey,
		targetSigningKey,
	)

	if err := c.failover(
		w,
		targetSigningKey,
	); err != nil {
		return err
	}

	c.state.LastTotalMissed =
		w.TotalMissed

	c.state.LastFailoverAt =
		time.Now().UTC().Format(time.RFC3339)

	c.state.LastFailoverFrom =
		w.SigningKey

	c.state.LastFailoverTo =
		targetSigningKey

	if err := c.saveState(); err != nil {
		return err
	}

	if targetIndex ==
		len(c.cfg.SigningKeys)-1 {

		log.Printf(
			"FAILSAFE signing key confirmed. Monitoring will be stopped.",
		)

		return ErrMonitoringStopped
	}

	return nil
}

// ------------------------------------------------------------
// Last Generated Block / Block Age
// ------------------------------------------------------------

func (c *Controller) logLastGeneratedBlock(
	w *Witness,
) {

	blockNum :=
		w.LastConfirmedBlockNum

	if blockNum == 0 {

		log.Printf(
			"LAST GENERATED BLOCK: block=0 age=unknown",
		)

		return
	}

	block, err := c.getBlock(
		blockNum,
	)

	if err != nil {

		log.Printf(
			"Block Age unavailable: block=%d error=%v",
			blockNum,
			err,
		)

		return
	}

	blockTime, err :=
		parseBlockTimestamp(
			block.Timestamp,
		)

	if err != nil {

		log.Printf(
			"Block Age unavailable: block=%d timestamp=%q error=%v",
			blockNum,
			block.Timestamp,
			err,
		)

		return
	}

	// ------------------------------------------------------------
	// Block Age
	//
	// blockTimeはUTC。
	// 経過時間の計算はUTCのままでよい。
	// ------------------------------------------------------------

	age := time.Since(blockTime)

	if age < 0 {
		age = 0
	}

	// ------------------------------------------------------------
	// JST表示
	// ------------------------------------------------------------

	displayTime :=
		blockTime.In(c.jst)

	// ------------------------------------------------------------
	// 実際のブロック生成Witnessを確認
	// ------------------------------------------------------------

	if block.Witness != c.cfg.Witness {

		log.Printf(
			"LAST GENERATED BLOCK WARNING: block=%d witness=%s expected=%s time=%s JST age=%s",
			blockNum,
			block.Witness,
			c.cfg.Witness,
			displayTime.Format(
				"2006-01-02 15:04:05",
			),
			formatElapsed(age),
		)

		return
	}

	log.Printf(
		"LAST GENERATED BLOCK: block=%d witness=%s time=%s JST age=%s",
		blockNum,
		block.Witness,
		displayTime.Format(
			"2006-01-02 15:04:05",
		),
		formatElapsed(age),
	)
}

// ------------------------------------------------------------
// getBlock
// ------------------------------------------------------------

func (c *Controller) getBlock(
	blockNum uint32,
) (*Block, error) {

	var result Block

	err := c.api.CallWithResult(
		"condenser_api",
		"get_block",
		[]interface{}{blockNum},
		&result,
	)

	if err != nil {

		c.recordRPCFailure()

		return nil, fmt.Errorf(
			"failed to get block %d: %w",
			blockNum,
			err,
		)
	}

	c.recordRPCSuccess()

	if result.Timestamp == "" {

		return nil, fmt.Errorf(
			"block %d returned empty timestamp",
			blockNum,
		)
	}

	return &result, nil
}

// ------------------------------------------------------------
// Block timestamp
// ------------------------------------------------------------

func parseBlockTimestamp(
	value string,
) (time.Time, error) {

	// Steem block timestampはUTC
	//
	// 例:
	// 2026-08-20T14:13:39

	return time.Parse(
		"2006-01-02T15:04:05",
		value,
	)
}

// ------------------------------------------------------------
// Block Age表示
// ------------------------------------------------------------

func formatElapsed(
	d time.Duration,
) string {

	if d < 0 {
		d = 0
	}

	hours :=
		int(d / time.Hour)

	minutes :=
		int((d % time.Hour) / time.Minute)

	seconds :=
		int((d % time.Minute) / time.Second)

	return fmt.Sprintf(
		"%d:%02d:%02d",
		hours,
		minutes,
		seconds,
	)
}

// ------------------------------------------------------------
// Failover
// ------------------------------------------------------------

func (c *Controller) failover(
	w *Witness,
	targetSigningKey string,
) error {

	log.Printf(
		"Changing witness signing key: %s -> %s",
		w.SigningKey,
		targetSigningKey,
	)

	op := &protocol.WitnessUpdateOperation{
		Owner:           c.cfg.Witness,
		URL:             w.URL,
		BlockSigningKey: targetSigningKey,

		Props: &protocol.ChainProperties{
			AccountCreationFee: w.Props.AccountCreationFee,
			MaximumBlockSize:   w.Props.MaximumBlockSize,
			SBDInterestRate:    w.Props.SBDInterestRate,
		},

		Fee: "0.000 STEEM",
	}

	result, err :=
		c.broadcastWithFailover(op)

	if err != nil {
		return err
	}

	log.Printf(
		"witness_update broadcast accepted: %s",
		string(result),
	)

	if err := c.waitForSigningKey(
		targetSigningKey,
		c.cfg.VerifyTimeout,
	); err != nil {
		return err
	}

	log.Printf(
		"FAILOVER SUCCESS: signing_key is now %s",
		targetSigningKey,
	)

	return nil
}

// ------------------------------------------------------------
// Broadcast with RPC endpoint retry
// ------------------------------------------------------------

func (c *Controller) broadcastWithFailover(
	op *protocol.WitnessUpdateOperation,
) ([]byte, error) {

	for {

		result, err :=
			c.bcast.SendWith(
				op,
				c.cfg.ActivePrivateKey,
			)

		if err == nil {

			c.recordRPCSuccess()

			return result, nil
		}

		c.recordRPCFailure()

		log.Printf(
			"witness_update broadcast RPC error: endpoint=%s error=%v",
			c.cfg.RPCURLs[c.currentRPCIndex],
			err,
		)

		if c.rpcFailureCount == 0 {

			return nil, fmt.Errorf(
				"failed to broadcast witness_update: all RPC endpoints failed: %w",
				err,
			)
		}

		time.Sleep(time.Second)
	}
}

// ------------------------------------------------------------
// Verify signing key
// ------------------------------------------------------------

func (c *Controller) waitForSigningKey(
	expected string,
	timeout time.Duration,
) error {

	deadline :=
		time.Now().Add(timeout)

	for time.Now().Before(deadline) {

		w, err := c.getWitness()

		if err != nil {

			log.Printf(
				"verification RPC error: %v",
				err,
			)

			time.Sleep(time.Second)

			continue
		}

		if w.SigningKey == expected {
			return nil
		}

		log.Printf(
			"waiting for signing key change: current=%s expected=%s",
			w.SigningKey,
			expected,
		)

		time.Sleep(time.Second)
	}

	return fmt.Errorf(
		"timeout waiting for signing key to become %s",
		expected,
	)
}

// ------------------------------------------------------------
// getWitness
// ------------------------------------------------------------

func (c *Controller) getWitness() (*Witness, error) {

	var lastErr error

	for {

		var result Witness

		err := c.api.CallWithResult(
			"condenser_api",
			"get_witness_by_account",
			[]interface{}{c.cfg.Witness},
			&result,
		)

		if err == nil {

			if result.Owner == "" {

				lastErr = fmt.Errorf(
					"witness not found: %s",
					c.cfg.Witness,
				)

				c.recordRPCSuccess()

				return nil, lastErr
			}

			c.recordRPCSuccess()

			return &result, nil
		}

		lastErr = err

		c.recordRPCFailure()

		log.Printf(
			"RPC error for condenser_api.get_witness_by_account: endpoint=%s error=%v",
			c.cfg.RPCURLs[c.currentRPCIndex],
			err,
		)

		if c.rpcFailureCount == 0 {

			if c.currentRPCIndex ==
				len(c.cfg.RPCURLs)-1 {

				log.Printf(
					"All RPC endpoints failed for get_witness_by_account.",
				)

				return nil, fmt.Errorf(
					"all RPC endpoints failed for get_witness_by_account: %w",
					lastErr,
				)
			}
		}

		time.Sleep(
			200 * time.Millisecond,
		)
	}
}

// ------------------------------------------------------------
// State
// ------------------------------------------------------------

func loadState(
	filename string,
) (State, error) {

	data, err :=
		os.ReadFile(filename)

	if err != nil {

		if os.IsNotExist(err) {
			return State{}, nil
		}

		return State{}, err
	}

	var state State

	if err := json.Unmarshal(
		data,
		&state,
	); err != nil {

		return State{}, fmt.Errorf(
			"invalid state file: %w",
			err,
		)
	}

	return state, nil
}

func (c *Controller) saveState() error {

	data, err :=
		json.MarshalIndent(
			c.state,
			"",
			"  ",
		)

	if err != nil {
		return err
	}

	tmp :=
		c.cfg.StateFile + ".tmp"

	if err := os.WriteFile(
		tmp,
		data,
		0600,
	); err != nil {
		return err
	}

	if err := os.Rename(
		tmp,
		c.cfg.StateFile,
	); err != nil {
		return err
	}

	return nil
}

// ------------------------------------------------------------
// Environment helpers
// ------------------------------------------------------------

func envString(
	name string,
	defaultValue string,
) string {

	value := os.Getenv(name)

	if value == "" {
		return defaultValue
	}

	return value
}

func envInt(
	name string,
	defaultValue int,
) int {

	value := os.Getenv(name)

	if value == "" {
		return defaultValue
	}

	n, err :=
		strconv.Atoi(value)

	if err != nil {
		return defaultValue
	}

	return n
}
