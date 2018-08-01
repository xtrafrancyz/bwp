package worker

import (
	"container/list"
	"log"
	"sync"
	"time"
)

type Pool struct {
	// Amount of workers in pool
	Size int
	// Amount of jobs that can be in queue
	QueueSize int

	handlers    map[string]JobHandler
	finish      bool
	jobsQueue   chan *job
	freeWorkers chan *worker
	workers     *list.List
}

type job struct {
	action string
	data   interface{}
}

type JobHandler = func(interface{}) error

func (p *Pool) Init() {
	p.handlers = make(map[string]JobHandler)
	p.jobsQueue = make(chan *job, p.QueueSize)
	p.freeWorkers = make(chan *worker, p.Size)
	p.workers = list.New()
}

func (p *Pool) Start() {
	for i := 0; i < p.Size; i++ {
		w := &worker{
			pool: p,
		}
		p.workers.PushFront(w)
		w.start()
	}

	go func() {
		for {
			select {
			// Wait for the jobs
			case job := <-p.jobsQueue:
				// Wait for the free worker
				w := <-p.freeWorkers

				// Send jot to worker
				w.jobsChan <- job
			}
		}
	}()
}

func (p *Pool) RegisterAction(action string, handler JobHandler) {
	p.handlers[action] = handler
}

func (p *Pool) AddJob(action string, data interface{}) {
	if p.finish {
		return
	}
	job := acquireJob()
	job.action = action
	job.data = data
	p.jobsQueue <- job
}

func (p *Pool) GetQueueLength() int {
	return len(p.jobsQueue)
}

func (p *Pool) GetActiveWorkers() int {
	return p.Size - len(p.freeWorkers)
}

func (p *Pool) Finish() {
	log.Println("Finishing all jobs...")
	p.finish = true
	for len(p.jobsQueue) != 0 {
		time.Sleep(50 * time.Millisecond)
	}
	for e := p.workers.Front(); e != nil; e = e.Next() {
		e.Value.(*worker).quit <- true
	}
	for {
		working := false
		for e := p.workers.Front(); e != nil; e = e.Next() {
			if !e.Value.(*worker).finished {
				working = true
			}
		}
		if !working {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
}

var jobPool sync.Pool

func acquireJob() *job {
	v := jobPool.Get()
	if v == nil {
		v = &job{}
	}
	return v.(*job)
}

func releaseJob(v *job) {
	v.action = ""
	v.data = nil
	jobPool.Put(v)
}
