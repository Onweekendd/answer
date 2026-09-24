/*
 * Licensed to the Apache Software Foundation (ASF) under one
 * or more contributor license agreements.  See the NOTICE file
 * distributed with this work for additional information
 * regarding copyright ownership.  The ASF licenses this file
 * to you under the Apache License, Version 2.0 (the
 * "License"); you may not use this file except in compliance
 * with the License.  You may obtain a copy of the License at
 *
 *   http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

// Package purchaseaiqueue hands AI analysis off to a background worker, so the
// request that triggered it does not wait tens of seconds for the provider.
package purchaseaiqueue

import (
	"context"
	"sync"
	"time"

	"github.com/apache/answer/internal/schema"
	"github.com/segmentfault/pacman/log"
)

// Service is the seam between "something wants an analysis" and "an analysis
// happens". Everything the rest of the code knows is these three methods, so
// replacing the implementation with a database backed queue in the persistent
// queue step changes this file only.
//
// Send must not block for long: it is called from inside an HTTP request.
// RegisterHandler is called once at startup, by PurchaseAIService's constructor.
// Close is graceful: it stops accepting messages and waits for the current one.
type Service interface {
	Send(ctx context.Context, msg *schema.PurchaseAIMsg)
	RegisterHandler(handler func(ctx context.Context, msg *schema.PurchaseAIMsg) error)
	Close()
}

// memoryQueue runs analyses one at a time on a single goroutine. Its backlog
// lives in memory, so it is lost on restart, and a failed job is not retried.
type memoryQueue struct {
	name  string
	queue chan *schema.PurchaseAIMsg
	wg    sync.WaitGroup

	// mu guards handler and closed
	mu      sync.RWMutex
	handler func(ctx context.Context, msg *schema.PurchaseAIMsg) error
	closed  bool
}

// NewService starts the worker and returns the queue. The buffer is small on
// purpose: an analysis costs money and takes tens of seconds, so a deep backlog
// means something is wrong rather than that more room is needed.
func NewService() Service {
	purchaseAIQueue := &memoryQueue{
		name:  "purchase_ai",
		queue: make(chan *schema.PurchaseAIMsg, 32),
	}

	purchaseAIQueue.wg.Add(1)
	go purchaseAIQueue.run()

	return purchaseAIQueue
}

func (q *memoryQueue) run() {
	defer q.wg.Done()
	for msg := range q.queue {
		q.process(msg)
	}
}

func (q *memoryQueue) process(msg *schema.PurchaseAIMsg) {
	q.mu.RLock()
	handler := q.handler
	q.mu.RUnlock()

	if handler == nil {
		log.Warnf("[%s] no handler registered, dropping message: %+v", q.name, msg)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	if err := handler(ctx, msg); err != nil {
		log.Errorf("[%s] handler error: %v", q.name, err)
	}

}

func (q *memoryQueue) RegisterHandler(handler func(ctx context.Context, msg *schema.PurchaseAIMsg) error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.handler = handler
}

func (q *memoryQueue) Send(ctx context.Context, msg *schema.PurchaseAIMsg) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	if q.closed {
		log.Warnf("[%s] queue is closed, dropping message: %+v", q.name, msg)
		return
	}

	select {
	case q.queue <- msg:
		log.Debugf("[%s] enqueued: %+v", q.name, msg)
	case <-ctx.Done():
		// the queue was full and the caller went away while waiting for room
		log.Warnf("[%s] caller gave up before the message was queued: %+v", q.name, msg)
	}
}

func (q *memoryQueue) Close() {
	q.mu.Lock()
	if q.closed {
		log.Warnf("[%s] queue already closed", q.name)
		q.mu.Unlock()
		return
	}
	q.closed = true
	q.mu.Unlock()

	close(q.queue)
	q.wg.Wait()
	log.Infof("[%s] queue closed", q.name)
}
