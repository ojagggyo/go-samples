package main

import (
	"log"
	"sync"
	"time"
)

type loadProgress struct {
	mu      sync.Mutex
	phase   string
	count   int
	started time.Time
}

func startLoadProgress() (*loadProgress, func()) {
	p := &loadProgress{phase: "キャッシュを読み込み中", started: time.Now()}
	p.report()
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				p.report()
			case <-stop:
				return
			}
		}
	}()
	return p, func() { close(stop); <-done }
}

func (p *loadProgress) setPhase(phase string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.phase, p.count = phase, 0
	p.mu.Unlock()
	p.report()
}

func (p *loadProgress) advance() {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.count++
	p.mu.Unlock()
}

func (p *loadProgress) report() {
	p.mu.Lock()
	defer p.mu.Unlock()
	log.Printf("%s… 確認済み=%d件 / 経過=%s", p.phase, p.count, time.Since(p.started).Round(time.Second))
}
