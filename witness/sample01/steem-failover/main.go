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

const Version = "1.0.0"

// FailsafeSigningKeyになったため監視を停止する。
var ErrMonitoringStopped = errors.New("monitoring stopped")

type Config struct {
	RPCURL           string
	Witness          string
	ActivePrivateKey string

	PrimarySigningKey  string
	BackupSigningKey   string
	FailsafeSigningKey string

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
	cfg   Config
	api   *api.API
	bcast *broadcast.Broadcast

	mu    sync.Mutex
	state State
}

func main() {

	// --version または -v
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--version", "-v":
			fmt.Printf("Steem witness failover controller version %s\n", Version)
			return
		}
	}

	cfg, err := loadConfig()
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("Starting Steem witness failover controller")
	log.Printf("Version   = %s", Version)
	log.Printf("RPC       = %s", cfg.RPCURL)
	log.Printf("Witness   = %s", cfg.Witness)
	log.Printf("Primary   = %s", cfg.PrimarySigningKey)
	log.Printf("Backup    = %s", cfg.BackupSigningKey)
	log.Printf("Failsafe  = %s", cfg.FailsafeSigningKey)
	log.Printf("Interval  = %s", cfg.PollInterval)

	state, err := loadState(cfg.StateFile)
	if err != nil {
		log.Fatal(err)
	}

	c := &Controller{
		cfg:   cfg,
		api:   api.NewAPI(cfg.RPCURL),
		bcast: broadcast.NewBroadcast(cfg.RPCURL),
		state: state,
	}

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
		RPCURL:           envString("STEEM_RPC_URL", "https://api.steemit.com"),
		Witness:          os.Getenv("STEEM_WITNESS"),
		ActivePrivateKey: os.Getenv("STEEM_ACTIVE_PRIVATE_KEY"),

		PrimarySigningKey:  os.Getenv("STEEM_PRIMARY_SIGNING_KEY"),
		BackupSigningKey:   os.Getenv("STEEM_BACKUP_SIGNING_KEY"),
		FailsafeSigningKey: os.Getenv("STEEM_FAILSAFE_SIGNING_KEY"),

		PollInterval: time.Duration(pollSeconds) * time.Second,

		StateFile: envString(
			"STATE_FILE",
			"state.json",
		),

		VerifyTimeout: time.Duration(
			envInt("VERIFY_TIMEOUT_SECONDS", 60),
		) * time.Second,
	}

	if cfg.Witness == "" {
		return cfg, errors.New("STEEM_WITNESS is required")
	}

	if cfg.ActivePrivateKey == "" {
		return cfg, errors.New("STEEM_ACTIVE_PRIVATE_KEY is required")
	}

	if cfg.PrimarySigningKey == "" {
		return cfg, errors.New("STEEM_PRIMARY_SIGNING_KEY is required")
	}

	if cfg.BackupSigningKey == "" {
		return cfg, errors.New("STEEM_BACKUP_SIGNING_KEY is required")
	}

	if cfg.FailsafeSigningKey == "" {
		return cfg, errors.New("STEEM_FAILSAFE_SIGNING_KEY is required")
	}

	if cfg.PrimarySigningKey == cfg.BackupSigningKey {
		return cfg, errors.New(
			"primary and backup signing keys must be different",
		)
	}

	if cfg.PrimarySigningKey == cfg.FailsafeSigningKey {
		return cfg, errors.New(
			"primary and failsafe signing keys must be different",
		)
	}

	if cfg.BackupSigningKey == cfg.FailsafeSigningKey {
		return cfg, errors.New(
			"backup and failsafe signing keys must be different",
		)
	}

	return cfg, nil
}

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

func (c *Controller) check(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	w, err := c.getWitness()
	if err != nil {
		return err
	}

	log.Printf(
		"witness=%s signing_key=%s total_missed=%d last=%d",
		c.cfg.Witness,
		w.SigningKey,
		w.TotalMissed,
		c.state.LastTotalMissed,
	)

	// ------------------------------------------------------------
	// FAILSAFE
	// ------------------------------------------------------------
	// Failsafeキーまで到達したら監視を終了する。
	if w.SigningKey == c.cfg.FailsafeSigningKey {

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
	// BACKUP
	// ------------------------------------------------------------
	if w.SigningKey == c.cfg.BackupSigningKey {

		if w.TotalMissed <= c.state.LastTotalMissed {
			c.state.LastTotalMissed = w.TotalMissed
			return c.saveState()
		}

		log.Printf(
			"BACKUP MISS DETECTED: total_missed %d -> %d",
			c.state.LastTotalMissed,
			w.TotalMissed,
		)

		log.Printf("Failing over Backup -> Failsafe")

		if err := c.failover(
			w,
			c.cfg.FailsafeSigningKey,
		); err != nil {
			return err
		}

		c.state.LastTotalMissed = w.TotalMissed
		c.state.LastFailoverAt = time.Now().UTC().Format(time.RFC3339)
		c.state.LastFailoverFrom = c.cfg.BackupSigningKey
		c.state.LastFailoverTo = c.cfg.FailsafeSigningKey

		if err := c.saveState(); err != nil {
			return err
		}

		// failover() 内でBlockchain上のSigningKey変更確認済み。
		log.Printf(
			"FAILSAFE signing key confirmed. Monitoring will be stopped.",
		)

		return ErrMonitoringStopped
	}

	// ------------------------------------------------------------
	// PRIMARY
	// ------------------------------------------------------------
	if w.SigningKey == c.cfg.PrimarySigningKey {

		if w.TotalMissed <= c.state.LastTotalMissed {
			c.state.LastTotalMissed = w.TotalMissed
			return c.saveState()
		}

		log.Printf(
			"FAILOVER condition detected: total_missed %d -> %d",
			c.state.LastTotalMissed,
			w.TotalMissed,
		)

		log.Printf("Failing over Primary -> Backup")

		if err := c.failover(
			w,
			c.cfg.BackupSigningKey,
		); err != nil {
			return err
		}

		c.state.LastTotalMissed = w.TotalMissed
		c.state.LastFailoverAt = time.Now().UTC().Format(time.RFC3339)
		c.state.LastFailoverFrom = c.cfg.PrimarySigningKey
		c.state.LastFailoverTo = c.cfg.BackupSigningKey

		return c.saveState()
	}

	// ------------------------------------------------------------
	// UNEXPECTED KEY
	// ------------------------------------------------------------
	log.Printf(
		"CRITICAL: unexpected signing key detected: blockchain=%s primary=%s backup=%s failsafe=%s",
		w.SigningKey,
		c.cfg.PrimarySigningKey,
		c.cfg.BackupSigningKey,
		c.cfg.FailsafeSigningKey,
	)

	return fmt.Errorf(
		"unexpected signing key: blockchain=%s primary=%s backup=%s failsafe=%s",
		w.SigningKey,
		c.cfg.PrimarySigningKey,
		c.cfg.BackupSigningKey,
		c.cfg.FailsafeSigningKey,
	)
}

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

	result, err := c.bcast.SendWith(
		op,
		c.cfg.ActivePrivateKey,
	)
	if err != nil {
		return fmt.Errorf(
			"failed to broadcast witness_update: %w",
			err,
		)
	}

	log.Printf(
		"witness_update broadcast accepted: %s",
		string(result),
	)

	// Blockchain上で本当に変更されたことを確認する。
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

func (c *Controller) getWitness() (*Witness, error) {
	var result Witness

	err := c.api.CallWithResult(
		"condenser_api",
		"get_witness_by_account",
		[]interface{}{c.cfg.Witness},
		&result,
	)
	if err != nil {
		return nil, err
	}

	if result.Owner == "" {
		return nil, fmt.Errorf(
			"witness not found: %s",
			c.cfg.Witness,
		)
	}

	return &result, nil
}

func loadState(filename string) (State, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return State{}, nil
		}

		return State{}, err
	}

	var state State

	if err := json.Unmarshal(data, &state); err != nil {
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
