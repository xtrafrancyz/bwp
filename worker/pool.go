package worker

import (
	"container/list"
	"errors"
	"log"
	"sync"
	"time"
)

var (
	ErrPoolClosed = errors.New("pool is closed")
	ErrQueueFull  = errors.New("queue is full")
)

type Pool struct {
	// Amount of workers in pool
	Size int
	// Amount of jobs that can be in queue
	QueueSize int

	handlers    map[string]JobHandler
	finish      bool
	jobsQueue   chan job
	freeWorkers chan *worker
	workers     *list.List
}

type job struct {
	action string
	data   any
}

type JobHandler = func(any) error

func (p *Pool) Init() {
	p.handlers = make(map[string]JobHandler)
	p.jobsQueue = make(chan job, p.QueueSize)
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
		for job := range p.jobsQueue {
			// Wait for the free worker
			w := <-p.freeWorkers

			// Send job to worker
			w.jobsChan <- job
		}
	}()
}

func (p *Pool) RegisterAction(action string, handler JobHandler) {
	p.handlers[action] = handler
}

func (p *Pool) AddJob(action string, data any) error {
	if p.finish {
		return ErrPoolClosed
	}
	select {
	case p.jobsQueue <- job{
		action: action,
		data:   data,
	}:
	default:
		return ErrQueueFull
	}
	return nil
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
	wg := &sync.WaitGroup{}
	wg.Add(p.Size)
	for e := p.workers.Front(); e != nil; e = e.Next() {
		e.Value.(*worker).quit <- wg
	}
	wg.Wait()
}
