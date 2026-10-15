package scheduler

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"sila/internal/database"
	"sila/internal/meta"
	"sila/internal/youtube"
)

type WorkerPool struct {
	maxConcurrency int
	semaphore      chan struct{}
	repo           *database.Repository
	metaPub        *meta.Publisher
	ytUpload       *youtube.Uploader
	wg             sync.WaitGroup
}

func NewWorkerPool(maxConcurrency int, repo *database.Repository, metaPub *meta.Publisher, ytUpload *youtube.Uploader) *WorkerPool {
	if maxConcurrency <= 0 {
		maxConcurrency = 2
	}
	return &WorkerPool{
		maxConcurrency: maxConcurrency,
		semaphore:      make(chan struct{}, maxConcurrency),
		repo:           repo,
		metaPub:        metaPub,
		ytUpload:       ytUpload,
	}
}

// ProcessPost handles executing a single post queue item
func (wp *WorkerPool) ProcessPost(item *database.PostQueueItem) {
	wp.semaphore <- struct{}{}
	wp.wg.Add(1)

	go func() {
		defer func() {
			<-wp.semaphore
			wp.wg.Done()
		}()

		log.Printf("[SILA Scheduler] Memproses postingan %s (Platform: %s, Tipe: %s)...", item.ID, item.Platform, item.PostType)
		_ = wp.repo.UpdatePostStatus(item.ID, "processing", "", "", "")

		acc, err := wp.repo.GetAccountByID(item.AccountID)
		if err != nil || acc == nil {
			errStr := fmt.Sprintf("Akun %s tidak ditemukan di database SILA", item.AccountID)
			log.Printf("[SILA Scheduler Error] %s", errStr)
			_ = wp.repo.UpdatePostStatus(item.ID, "failed", errStr, "", "")
			return
		}

		if item.Platform == "youtube" {
			res := wp.ytUpload.UploadVideo(acc, item)
			if res.Success {
				log.Printf("[SILA Scheduler] Sukses publish YouTube %s -> Video ID: %s", item.ID, res.VideoID)
				_ = wp.repo.UpdatePostStatus(item.ID, "published", "", res.VideoID, res.URL)
			} else {
				log.Printf("[SILA Scheduler Error] Gagal publish YouTube %s: %s", item.ID, res.Error)
				_ = wp.repo.UpdatePostStatus(item.ID, "failed", res.Error, "", "")
			}
			return
		}

		// Meta (Facebook, Instagram, Threads)
		res := wp.metaPub.PublishPost(acc, item)
		if res.Success {
			log.Printf("[SILA Scheduler] Sukses publish Meta %s (%s) -> Post ID: %s", item.ID, item.Platform, res.PostID)
			_ = wp.repo.UpdatePostStatus(item.ID, "published", "", res.PostID, res.PostURL)
		} else {
			log.Printf("[SILA Scheduler Error] Gagal publish Meta %s (%s): %s", item.ID, item.Platform, res.Error)
			_ = wp.repo.UpdatePostStatus(item.ID, "failed", res.Error, "", "")
		}
	}()
}

func (wp *WorkerPool) Wait() {
	wp.wg.Wait()
}
