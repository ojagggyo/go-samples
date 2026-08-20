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

const Version = "1.2.0"

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

	Props struct {
		AccountCreationFee string `json:"account_creation_fee"`
		MaximumBlockSize   uint32 `json:"maximum_block_size"`
		SBDInterestRate    uint16 `json:"sbd_interest_rate"`
	} `json:"props"`
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

	// 現在使用しているRPC URLのインデックス
	currentRPCIndex int

	// 現在のRPC URLで連続して失敗した回数
	rpcFailureCount int

	api   *api.API
	bcast *broadcast.Broadcast
}

func main() {

	// --version または -v
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

	log.Printf("Starting Steem witness failover controller")
	log.Printf("Version   = %s", Version)

	for i, url := range cfg.RPCURLs {
		log.Printf("RPC[%d]    = %s", i+1, url)
	}

	log.Printf("Witness   = %s", cfg.Witness)
	for i, key := range cfg.SigningKeys {
		log.Printf("SigningKey[%d] = %s", i+1, key)
	}
	log.Printf("Interval  = %s", cfg.PollInterval)

	state, err := loadState(cfg.StateFile)
	if err != nil {
		log.Fatal(err)
	}

	c := &Controller{
		cfg:             cfg,
		state:           state,
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
		if errors.Is(err, ErrMonitoringStopped) {
			log.Printf("Monitoring stopped.")
			return
		}

		log.Printf("check error: %v", err)
	}

	for {
		select {
		case <-ctx.Done():
			log.Printf("Stopping.")
			return

		case <-ticker.C:
			if err := c.check(ctx); err != nil {
				if errors.Is(err, ErrMonitoringStopped) {
					log.Printf("Monitoring stopped.")
					return
				}

				log.Printf("check error: %v", err)
			}
		}
	}
}

func loadConfig() (Config, error) {

	pollSeconds := envInt("POLL_SECONDS", 2)

	cfg := Config{
		Witness:          os.Getenv("STEEM_WITNESS"),
		ActivePrivateKey: os.Getenv("STEEM_ACTIVE_PRIVATE_KEY"),

		PollInterval: time.Duration(pollSeconds) * time.Second,

		StateFile: envString(
			"STATE_FILE",
			"state.json",
		),

		VerifyTimeout: time.Duration(
			envInt("VERIFY_TIMEOUT_SECONDS", 60),
		) * time.Second,
	}

	// ------------------------------------------------------------
	// RPC URLs
	// ------------------------------------------------------------
	//
	// 新方式:
	//
	// STEEM_RPC_URL_1=https://api.steemit.com
	// STEEM_RPC_URL_2=https://...
	// STEEM_RPC_URL_3=https://...
	//
	// 旧方式:
	//
	// STEEM_RPC_URL=https://api.steemit.com
	//
	// STEEM_RPC_URL_1 がない場合は STEEM_RPC_URL を使用。
	//
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
	//
	// New:
	//   STEEM_SIGNING_KEY_1=STM...Primary
	//   STEEM_SIGNING_KEY_2=STM...aaa
	//   STEEM_SIGNING_KEY_3=STM...bbb
	//   STEEM_SIGNING_KEY_4=STM...Failsafe
	//
	// The chain must contain at least two different keys.
	// The last key is treated as the terminal/failsafe key.
	// ------------------------------------------------------------

	for i := 1; ; i++ {
		name := fmt.Sprintf("STEEM_SIGNING_KEY_%d", i)
		key := os.Getenv(name)
		if key == "" {
			break
		}
		cfg.SigningKeys = append(cfg.SigningKeys, key)
	}

	// Backward compatibility with the v1.1.0 environment variables.
	if len(cfg.SigningKeys) == 0 {
		primary := os.Getenv("STEEM_PRIMARY_SIGNING_KEY")
		backup := os.Getenv("STEEM_BACKUP_SIGNING_KEY")
		failsafe := os.Getenv("STEEM_FAILSAFE_SIGNING_KEY")

		if primary != "" {
			cfg.SigningKeys = append(cfg.SigningKeys, primary)
		}
		if backup != "" {
			cfg.SigningKeys = append(cfg.SigningKeys, backup)
		}
		if failsafe != "" {
			cfg.SigningKeys = append(cfg.SigningKeys, failsafe)
		}
	}

	if len(cfg.SigningKeys) < 2 {
		return cfg, errors.New(
			"at least two signing keys are required: STEEM_SIGNING_KEY_1, STEEM_SIGNING_KEY_2, ...",
		)
	}

	// Duplicate key check.
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

	if index < 0 || index >= len(c.cfg.RPCURLs) {
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
//
// 同じURLで3回連続失敗したら、次のRPC URLへ切り替える。
// ただし、RPC失敗そのものではFailoverしない。
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

	// 次のRPC URLが存在する場合
	if c.currentRPCIndex+1 < len(c.cfg.RPCURLs) {

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

	// 3 URLすべて失敗
	log.Printf(
		"All configured RPC endpoints have failed.",
	)

	// 現在のURLでカウンターを0に戻す。
	// 重要:
	// API障害だけではFailoverしない。
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
		"Current witness state: signing_key=%s total_missed=%d",
		w.SigningKey,
		w.TotalMissed,
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
		// --------------------------------------------------------
		// 重要:
		//
		// API/RPC失敗ではFailoverしない。
		//
		// MISSを取得できていないため、
		// Failover条件そのものが成立していない。
		// --------------------------------------------------------
		return err
	}

	log.Printf(
		"witness=%s signing_key=%s total_missed=%d last=%d rpc=%s",
		c.cfg.Witness,
		w.SigningKey,
		w.TotalMissed,
		c.state.LastTotalMissed,
		c.cfg.RPCURLs[c.currentRPCIndex],
	)

	// ------------------------------------------------------------
	// Find current signing key in the configured failover chain.
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
	// Last key is the terminal/failsafe key.
	// ------------------------------------------------------------

	if currentIndex == len(c.cfg.SigningKeys)-1 {
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

	if w.TotalMissed <= c.state.LastTotalMissed {
		c.state.LastTotalMissed = w.TotalMissed
		return c.saveState()
	}

	// ------------------------------------------------------------
	// 最重要:
	//
	// total_missed が「ちょうど +1」の場合だけFailover。
	// +2以上の場合はFailoverしない。
	// ------------------------------------------------------------

	if w.TotalMissed != c.state.LastTotalMissed+1 {
		log.Printf(
			"MISS increased by more than 1: %d -> %d. No failover.",
			c.state.LastTotalMissed,
			w.TotalMissed,
		)

		c.state.LastTotalMissed = w.TotalMissed
		return c.saveState()
	}

	targetIndex := currentIndex + 1
	targetSigningKey := c.cfg.SigningKeys[targetIndex]

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

	c.state.LastTotalMissed = w.TotalMissed
	c.state.LastFailoverAt = time.Now().UTC().Format(time.RFC3339)
	c.state.LastFailoverFrom = w.SigningKey
	c.state.LastFailoverTo = targetSigningKey

	if err := c.saveState(); err != nil {
		return err
	}

	// ------------------------------------------------------------
	// If the target is the last key, stop monitoring.
	// Otherwise continue with the next stage on the next check.
	// ------------------------------------------------------------

	if targetIndex == len(c.cfg.SigningKeys)-1 {
		log.Printf(
			"FAILSAFE signing key confirmed. Monitoring will be stopped.",
		)
		return ErrMonitoringStopped
	}

	return nil
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

	// ------------------------------------------------------------
	// Broadcast
	// ------------------------------------------------------------
	//
	// API障害の場合はFailover条件そのものを作らない。
	// ただし、すでにMISS +1が確認されてFailoverを実行中なら、
	// 現在のRPCでbroadcastできない場合は次のRPC endpointを使用する。
	// ------------------------------------------------------------

	result, err := c.broadcastWithFailover(
		op,
	)

	if err != nil {
		return err
	}

	log.Printf(
		"witness_update broadcast accepted: %s",
		string(result),
	)

	// ------------------------------------------------------------
	// Blockchain上で本当に変更されたことを確認する。
	// ------------------------------------------------------------

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

		result, err := c.bcast.SendWith(
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

		// --------------------------------------------------------
		// 現在のendpointが3回失敗した場合、
		// recordRPCFailure()が次のendpointへ切り替える。
		//
		// ただし全endpointが失敗した場合は、
		// API障害として終了。
		// --------------------------------------------------------

		if c.rpcFailureCount == 0 {

			// 全RPC endpointを試した
			return nil, fmt.Errorf(
				"failed to broadcast witness_update: all RPC endpoints failed: %w",
				err,
			)
		}

		// 少し待ってから再試行
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

	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {

		w, err := c.getWitness()

		if err != nil {

			log.Printf(
				"verification RPC error: %v",
				err,
			)

			// APIエラーではFailover状態を変更しない。
			// 次の確認を行う。
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
//
// 1つのRPC URLで3回連続失敗
//     ↓
// 次のRPC URL
//
// 最大3 URL
//
// 重要:
// RPC失敗ではFailover条件を作らない。
// ------------------------------------------------------------

func (c *Controller) getWitness() (*Witness, error) {

	var lastErr error

	// ------------------------------------------------------------
	// 最大で3 URLを使用
	// ------------------------------------------------------------

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

				// Witness not foundはRPC通信成功なので
				// RPC failure counterはリセット。
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

		// --------------------------------------------------------
		// 3回失敗して次のURLに切り替わった場合
		//
		// setRPC()によってrpcFailureCount=0になる。
		// --------------------------------------------------------

		if c.rpcFailureCount == 0 {

			// 全RPC URLが失敗した場合
			if c.currentRPCIndex == len(c.cfg.RPCURLs)-1 {

				log.Printf(
					"All RPC endpoints failed for get_witness_by_account.",
				)

				return nil, fmt.Errorf(
					"all RPC endpoints failed for get_witness_by_account: %w",
					lastErr,
				)
			}
		}

		// --------------------------------------------------------
		// 同じURLでまだ3回未満なら再試行
		// --------------------------------------------------------

		time.Sleep(200 * time.Millisecond)
	}
}

// ------------------------------------------------------------
// State
// ------------------------------------------------------------

func loadState(
	filename string,
) (State, error) {

	data, err := os.ReadFile(filename)

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

	data, err := json.MarshalIndent(
		c.state,
		"",
		"  ",
	)

	if err != nil {
		return err
	}

	tmp := c.cfg.StateFile + ".tmp"

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

	n, err := strconv.Atoi(value)

	if err != nil {
		return defaultValue
	}

	return n
}
