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

type memoryQueue struct {
	name  string
	queue chan *schema.PurchaseAIMsg
	wg    sync.WaitGroup

	// mu guards handler and closed
	mu      sync.RWMutex
	handler func(ctx context.Context, msg *schema.PurchaseAIMsg) error
	closed  bool
}

// NewService new purchase ai queue service.
// TODO(你来实现)
//
// 两条路，选一条：
//
//	A. 直接复用项目已有的泛型队列，一行：
//	     return queue.New[*schema.PurchaseAIMsg]("purchase_ai", 32)
//	   （import "github.com/apache/answer/internal/base/queue"；它的 Service[T]
//	   正好有上面这三个方法，接口直接就对上了。）
//
//	B. 自己写这个 worker —— 想练并发的话选这个：
//	     - 一个带缓冲的 channel 存待处理消息
//	     - 一个 goroutine range 这个 channel，取到就调 handler
//	     - handler 字段外面要加 sync.RWMutex：RegisterHandler 在主 goroutine 写，
//	       worker goroutine 读，不加锁就是数据竞争，go test -race 会报
//	     - 读锁里只拿 handler 的副本，解锁之后再调用。不要持着锁跑 handler，
//	       它要跑几十秒，期间谁都别想注册
//	     - handler 可能还是 nil（worker 比 RegisterHandler 先启动），要判
//	     - 一个 sync.WaitGroup，让 Close 能等正在处理的那条消息跑完
//	     - 一个 closed 标记，Close 之后再 Send 要丢弃，不能往已关闭的 channel 写（会 panic）
//	   写完再去读 internal/base/queue/queue.go 对一下，看差在哪。
//
// 两条路都要定的两件事：
//   - 缓冲区多大：一次分析要花真钱、跑几十秒，所以积压很深说明出问题了，
//     而不是说明需要更大的缓冲
//   - handler 拿到的是什么 context：绝对不能是调用方的，否则入队它的那个
//     HTTP 请求一返回，分析就被取消了
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
