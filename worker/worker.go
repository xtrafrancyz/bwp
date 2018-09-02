package worker

import (
	"log"
	"sync"
)

type worker struct {
	pool     *Pool
	jobsChan chan *job
	quit     chan *sync.WaitGroup
}

func (w *worker) start() {
	w.jobsChan = make(chan *job, 1)
	w.quit = make(chan *sync.WaitGroup, 1)

	go func() {
		for {
			w.pool.freeWorkers <- w

			select {
			case job := <-w.jobsChan:
				w.doJob(job)

			case wg := <-w.quit:
				wg.Done()
				return
			}
		}
	}()
}

func (w *worker) doJob(job *job) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("panic in job %s: %s", job.action, r)
		}
	}()

	if handler, ok := w.pool.handlers[job.action]; ok {
		if err := handler(job.data); err != nil {
			log.Printf("%s is failed: %s", job.action, err.Error())
		}
	} else {
		log.Printf("Unknown job action: %s", job.action)
	}
	releaseJob(job)
}
