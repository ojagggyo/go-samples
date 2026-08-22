package main

type Witness struct {
	Name             string `json:"name"`
	Votes            string `json:"votes"`
	RunningVersion   string `json:"running_version"`
	SigningKey       string `json:"signing_key"`
	TotalMissed      int64  `json:"total_missed"`
	LastUpdate       string `json:"last_update"`
	Miss             int64  `json:"miss"`
	SigningKeyChange int    `json:"signing_key_change"`
}
