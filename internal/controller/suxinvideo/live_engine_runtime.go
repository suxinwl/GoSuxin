package suxinvideo

import (
	"context"
	"github.com/suxinwl/GoSuxin/internal/extend/pluginlifecycle"
	"github.com/suxinwl/GoSuxin/internal/liveruntime"
	"sync"
	"time"
)

func liveEngineSecret() string             { return liveruntime.Secret() }
func liveProviderOrigin(key string) string { return liveruntime.Origin(key) }

var liveEngineStartupOnce sync.Once

func startLiveEngines(ctx context.Context) {
	pluginlifecycle.RegisterStop("suxinvideo", StopLiveEngines)
	liveruntime.Start(liveProviderAccountConfigured)
	liveEngineStartupOnce.Do(func() {
		go func() {
			initialized := map[string]int{}
			timer := time.NewTicker(5 * time.Second)
			defer timer.Stop()
			for range timer.C {
				for _, state := range liveruntime.States() {
					if !state.Healthy || state.PID == 0 || initialized[state.Profile] == state.PID {
						continue
					}
					jobCtx, cancel := context.WithTimeout(context.Background(), time.Minute)
					err := liveInitializeProvider(jobCtx, state.Profile)
					cancel()
					if err == nil {
						initialized[state.Profile] = state.PID
					}
				}
			}
		}()
	})
}
func StopLiveEngines() {
	liveruntime.Stop()
	liveBridges.Lock()
	defer liveBridges.Unlock()
	for key, bridge := range liveBridges.items {
		delete(liveBridges.items, key)
		_ = bridge.command.Process.Kill()
		bridge.release()
	}
}
