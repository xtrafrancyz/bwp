package worker

import "log"

type worker struct {
	pool     *Pool
	jobsChan chan *job
	quit     chan bool
	finished bool
}

func (w *worker) start() {
	w.jobsChan = make(chan *job, 1)
	w.quit = make(chan bool, 1)

	go func() {
		for {
			w.pool.freeWorkers <- w

			select {
			case job := <-w.jobsChan:
				w.doJob(job)

			case <-w.quit:
				w.finished = true
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
			log.Printf("Job(%s) is failed: %s", job.action, err.Error())
		}
	} else {
		log.Printf("Unknown job action: %s", job.action)
	}
}
