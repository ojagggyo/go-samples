mkdir steem-failover
cd steem-failover

go mod init steem-failover

go get github.com/steemit/steemgosdk

go build -o steem-failover .



windows版
set STEEM_RPC_URL=https://api.steemit.com
set STEEM_WITNESS=yasu.witness
set STEEM_PRIMARY_SIGNING_KEY=STM5DnNw8LWg6Q5TS4wivQ5pR5iufTQYPBjw4vYEWQG8Yof4aZAhJ
set STEEM_BACKUP_SIGNING_KEY=STM7gkLyT7mkXGj1NVDgLRdrpr5UeMCviv4rkPH9gXiQ8KsYjuhaU
set POLL_SECONDS=2
set STATE_FILE=state.json
set DRY_RUN=true
set VERIFY_TIMEOUT_SECONDS=60

切替テスト
set TEST_FAILOVER=false


sudo nano /etc/systemd/system/steem-failover.service

----------------------------------------------------------------------
[Unit]
Description=Steem Witness Failover Controller
After=network-online.target
Wants=network-online.target

[Service]
Type=simple

User=steem
Group=steem

WorkingDirectory=/home/steem/github/go-samples/witness/sample01/steem-failover
EnvironmentFile=/home/steem/github/go-samples/witness/sample01/steem-failover/steem-failover.env
ExecStart=/home/steem/github/go-samples/witness/sample01/steem-failover/steem-failover

Restart=always
RestartSec=5

NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
----------------------------------------------------------------------

sudo systemctl daemon-reload

sudo systemctl start steem-failover
sudo systemctl stop steem-failover
sudo systemctl status steem-failover
sudo systemctl enable steem-failover
Created symlink /etc/systemd/system/multi-user.target.wants/steem-failover.service → /etc/systemd/system/steem-failover.service.
sudo systemctl is-enabled steem-failover
sudo journalctl -u steem-failover -f

