// Licensed to the Apache Software Foundation (ASF) under one
// or more contributor license agreements. See the NOTICE file
// distributed with this work for additional information regarding
// copyright ownership. The ASF licenses this file to You under the
// Apache License, Version 2.0 (the "License"); you may not use this
// file except in compliance with the License. You may obtain a copy
// at http://www.apache.org/licenses/LICENSE-2.0
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.

package assets

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Service serializes jobs that share one logged-in browser. Job manifests are
// persisted; interrupted work is marked failed on restart, never completed.
type Service struct {
	mu      sync.Mutex
	jobs    map[string]*Job
	cancels map[string]context.CancelFunc
	dir     string
	run     RunFunc
	store   Store
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	queue   chan string
	closed  bool
}

func NewService(dir string, run RunFunc, store Store) (*Service, error) {
	if run == nil || store == nil {
		return nil, errors.New("runner and store are required")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Service{jobs: map[string]*Job{}, cancels: map[string]context.CancelFunc{}, dir: dir, run: run, store: store, ctx: ctx, cancel: cancel, queue: make(chan string, 100)}
	entries, err := os.ReadDir(dir)
	if err != nil {
		cancel()
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			cancel()
			return nil, err
		}
		var j Job
		if err := json.Unmarshal(data, &j); err != nil {
			cancel()
			return nil, err
		}
		if !identifier.MatchString(j.ID) || entry.Name() != j.ID+".json" {
			cancel()
			return nil, errors.New("invalid task manifest")
		}
		if j.Status == Running || j.Status == Queued {
			j.Status, j.Error = Failed, "service restarted before this task finished; submit a new task"
			j.UpdatedAt = time.Now().UTC()
			if err := s.save(&j); err != nil {
				cancel()
				return nil, err
			}
		}
		s.jobs[j.ID] = &j
	}
	s.wg.Add(1)
	go s.worker()
	return s, nil
}

func clone(j *Job) Job {
	out := *j
	out.Assets = append([]Asset{}, j.Assets...)
	out.Request.SourceURLs = append([]string(nil), j.Request.SourceURLs...)
	return out
}

// save is called under mu (or before the worker starts).
func (s *Service) save(j *Job) error {
	data, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return err
	}
	p := filepath.Join(s.dir, j.ID+".json")
	if err := os.WriteFile(p+".tmp", data, 0600); err != nil {
		return err
	}
	return os.Rename(p+".tmp", p)
}

func (s *Service) Create(req Request) (Job, error) {
	if err := req.normalize(); err != nil {
		return Job{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Job{}, ErrClosed
	}
	if req.RequestID != "" {
		for _, j := range s.jobs {
			if j.Request.ProjectID == req.ProjectID && j.Request.RequestID == req.RequestID {
				if !reflect.DeepEqual(j.Request, req) {
					return Job{}, ErrConflict
				}
				return clone(j), nil
			}
		}
	}
	if len(s.jobs) >= 1000 || len(s.queue) == cap(s.queue) {
		return Job{}, ErrCapacity
	}
	now := time.Now().UTC()
	j := &Job{ID: uuid.NewString(), Request: req, Status: Queued, Stage: "queued", Assets: []Asset{}, CreatedAt: now, UpdatedAt: now}
	if err := s.save(j); err != nil {
		return Job{}, err
	}
	s.jobs[j.ID] = j
	s.queue <- j.ID
	return clone(j), nil
}

func (s *Service) Get(ctx context.Context, id string) (Job, error) {
	s.mu.Lock()
	j, ok := s.jobs[id]
	if !ok {
		s.mu.Unlock()
		return Job{}, ErrNotFound
	}
	out := clone(j)
	s.mu.Unlock()
	for i, a := range out.Assets {
		var err error
		out.Assets[i], err = s.store.AccessURL(ctx, a)
		if err != nil {
			return Job{}, err
		}
	}
	return out, nil
}

func terminal(status string) bool { return status != Queued && status != Running }

func (s *Service) Cancel(id string) (Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[id]
	if !ok {
		return Job{}, ErrNotFound
	}
	if !terminal(j.Status) {
		j.Status, j.Stage, j.UpdatedAt = Canceled, "canceled", time.Now().UTC()
		if cancel := s.cancels[id]; cancel != nil {
			cancel()
		}
		if err := s.save(j); err != nil {
			return Job{}, err
		}
	}
	return clone(j), nil
}

func (s *Service) worker() {
	defer s.wg.Done()
	for {
		select {
		case <-s.ctx.Done():
			return
		case id := <-s.queue:
			s.execute(id)
		}
	}
}

func (s *Service) execute(id string) {
	s.mu.Lock()
	j := s.jobs[id]
	if s.ctx.Err() != nil || j.Status != Queued {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithTimeout(s.ctx, 12*time.Minute)
	s.cancels[id] = cancel
	j.Status, j.Stage, j.UpdatedAt = Running, "starting", time.Now().UTC()
	if err := s.save(j); err != nil {
		j.Status, j.Error = Failed, "cannot persist task state"
		delete(s.cancels, id)
		cancel()
		s.mu.Unlock()
		return
	}
	input := clone(j)
	s.mu.Unlock()
	defer cancel()
	emit := func(a Asset) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		// An upload that raced cancellation is still a real object; retain its
		// manifest while keeping the task canceled.
		for _, existing := range j.Assets {
			if existing.ObjectKey == a.ObjectKey {
				return nil
			}
		}
		j.Assets = append(j.Assets, a)
		j.UpdatedAt = time.Now().UTC()
		return s.save(j)
	}
	progress := func(stage string) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if j.Status == Running {
			j.Stage = stage
			j.UpdatedAt = time.Now().UTC()
		}
	}
	out, err := s.run(ctx, input, emit, progress)
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.cancels, id)
	if j.Status != Canceled {
		switch {
		case err == nil && len(j.Assets) >= j.Request.Count:
			j.Status = Completed
		case out.NeedsInput:
			j.Status = InputRequired
		case len(j.Assets) > 0:
			j.Status = Partial
		default:
			j.Status = Failed
		}
		j.Error = out.Message
		if err != nil {
			j.Error = err.Error()
		}
		if j.Status == Completed {
			j.Error = ""
		}
		if j.Status == Failed && j.Error == "" {
			j.Error = "agent finished without uploading the requested assets"
		}
		j.Stage = j.Status
	}
	j.UpdatedAt = time.Now().UTC()
	if err := s.save(j); err != nil {
		j.Status, j.Error = Failed, "cannot persist final task state"
	}
}

func (s *Service) Close() {
	s.mu.Lock()
	s.closed = true
	s.cancel()
	s.mu.Unlock()
	s.wg.Wait()
}
