package scheduler

import (
	"context"
	"log"
	"time"

	"sila/internal/database"
)

type CronWorker struct {
	repo       *database.Repository
	workerPool *WorkerPool
	ticker     *time.Ticker
	stopCh     chan struct{}
}

func NewCronWorker(repo *database.Repository, pool *WorkerPool) *CronWorker {
	return &CronWorker{
		repo:       repo,
		workerPool: pool,
		stopCh:     make(chan struct{}),
	}
}

func (w *CronWorker) Start(ctx context.Context) {
	w.ticker = time.NewTicker(30 * time.Second)
	log.Println("[SILA Scheduler] Worker background dimulai (interval: 30 detik)...")

	// Run once immediately on start
	w.tick()

	go func() {
		for {
			select {
			case <-ctx.Done():
				w.Stop()
				return
			case <-w.stopCh:
				return
			case <-w.ticker.C:
				w.tick()
			}
		}
	}()
}

func (w *CronWorker) Stop() {
	if w.ticker != nil {
		w.ticker.Stop()
	}
	close(w.stopCh)
	w.workerPool.Wait()
	log.Println("[SILA Scheduler] Worker background berhenti dengan aman.")
}

func (w *CronWorker) tick() {
	now := time.Now().Unix()
	pending, err := w.repo.GetPendingPosts(now, 10)
	if err != nil {
		log.Printf("[SILA Scheduler Error] Gagal membaca antrean postingan: %v", err)
		return
	}

	if len(pending) > 0 {
		log.Printf("[SILA Scheduler] Menemukan %d postingan siap dieksekusi...", len(pending))
		for _, item := range pending {
			copyItem := item
			w.workerPool.ProcessPost(&copyItem)
		}
	}
}
