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
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type memoryStore struct {
	mu    sync.Mutex
	files map[string][]byte
	fail  bool
}

func (s *memoryStore) Put(_ context.Context, scope string, a Asset, data []byte) (Asset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail {
		return Asset{}, errors.New("upload refused")
	}
	if s.files == nil {
		s.files = map[string][]byte{}
	}
	a.ObjectKey, a.ID, a.Bucket, a.Bytes = scope+"/asset", "asset", "test", len(data)
	s.files[a.ObjectKey] = append([]byte{}, data...)
	return a, nil
}
func (s *memoryStore) AccessURL(_ context.Context, a Asset) (Asset, error) {
	a.URL = "https://store.test/" + a.ObjectKey
	a.URLExpiresAt = time.Now().Add(time.Hour)
	return a, nil
}

func request() Request {
	return Request{ProjectID: "test-project", Query: "mountains", Kind: "video", Count: 1}
}

func waitJob(t *testing.T, s *Service, id string) Job {
	t.Helper()
	var got Job
	require.Eventually(t, func() bool {
		var err error
		got, err = s.Get(context.Background(), id)
		return err == nil && terminal(got.Status)
	}, 3*time.Second, 5*time.Millisecond)
	return got
}

func TestServiceUsesActualAssetsAndPersists(t *testing.T) {
	for _, tc := range []struct {
		name      string
		emitCount int
		want      string
		outcome   Outcome
		runErr    error
	}{
		{"claimed_success_without_upload", 0, Failed, Outcome{}, nil},
		{"confirmed_upload", 1, Completed, Outcome{}, nil},
		{"partial_failure", 1, Partial, Outcome{}, errors.New("second upload failed")},
		{"human_login", 0, InputRequired, Outcome{NeedsInput: true, Message: "login required"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			run := func(_ context.Context, j Job, emit func(Asset) error, _ func(string)) (Outcome, error) {
				for i := 0; i < tc.emitCount; i++ {
					if err := emit(Asset{ObjectKey: "real/object", ID: "real"}); err != nil {
						return Outcome{}, err
					}
				}
				return tc.outcome, tc.runErr
			}
			s, err := NewService(dir, run, &memoryStore{})
			require.NoError(t, err)
			req := request()
			if tc.want == Partial {
				req.Count = 2
			}
			j, err := s.Create(req)
			require.NoError(t, err)
			got := waitJob(t, s, j.ID)
			require.Equal(t, tc.want, got.Status)
			require.Len(t, got.Assets, tc.emitCount)
			s.Close()
			restarted, err := NewService(dir, run, &memoryStore{})
			require.NoError(t, err)
			defer restarted.Close()
			got, err = restarted.Get(context.Background(), j.ID)
			require.NoError(t, err)
			require.Equal(t, tc.want, got.Status)
		})
	}
}

func TestIdempotencyAndCancel(t *testing.T) {
	started := make(chan struct{}, 2)
	run := func(ctx context.Context, _ Job, _ func(Asset) error, _ func(string)) (Outcome, error) {
		started <- struct{}{}
		<-ctx.Done()
		return Outcome{}, ctx.Err()
	}
	s, err := NewService(t.TempDir(), run, &memoryStore{})
	require.NoError(t, err)
	defer s.Close()
	req := request()
	req.RequestID = "retry-safe"
	first, err := s.Create(req)
	require.NoError(t, err)
	second, err := s.Create(req)
	require.NoError(t, err)
	require.Equal(t, first.ID, second.ID)
	req.Query = "different"
	_, err = s.Create(req)
	require.ErrorIs(t, err, ErrConflict)
	<-started
	queued, err := s.Create(request())
	require.NoError(t, err)
	_, err = s.Cancel(queued.ID)
	require.NoError(t, err)
	_, err = s.Cancel(first.ID)
	require.NoError(t, err)
	require.Equal(t, Canceled, waitJob(t, s, first.ID).Status)
	require.Equal(t, Canceled, waitJob(t, s, queued.ID).Status)
	_, err = s.Get(context.Background(), "missing")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestRestartDoesNotClaimQueuedJobsSucceeded(t *testing.T) {
	dir := t.TempDir()
	s, err := NewService(dir, func(ctx context.Context, _ Job, _ func(Asset) error, _ func(string)) (Outcome, error) {
		<-ctx.Done()
		return Outcome{}, ctx.Err()
	}, &memoryStore{})
	require.NoError(t, err)
	j := &Job{ID: "interrupted", Request: request(), Status: Running, Assets: []Asset{}}
	s.mu.Lock()
	require.NoError(t, s.save(j))
	s.mu.Unlock()
	s.Close()
	s, err = NewService(dir, func(context.Context, Job, func(Asset) error, func(string)) (Outcome, error) {
		t.Error("interrupted job must not restart automatically")
		return Outcome{}, nil
	}, &memoryStore{})
	require.NoError(t, err)
	defer s.Close()
	got, err := s.Get(context.Background(), j.ID)
	require.NoError(t, err)
	require.Equal(t, Failed, got.Status)
	require.Contains(t, got.Error, "restarted")
}
