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

// Package objectstore keeps files in an S3-compatible object store (Garage in
// production) instead of the local upload directory, so they live outside the
// container and its volume.
package objectstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Environment variables that configure the store. Without S3_ENDPOINT there is
// no store and callers keep using the local disk, as local development does.
const (
	envEndpoint = "S3_ENDPOINT"          // host:port, e.g. garage:3900
	envBucket   = "S3_BUCKET"            // default maibumai
	envKeyID    = "S3_ACCESS_KEY_ID"     // nolint:gosec // the name of a variable, not a key
	envSecret   = "S3_SECRET_ACCESS_KEY" // nolint:gosec
	envRegion   = "S3_REGION"            // Garage's default region is "garage"
	envUseSSL   = "S3_USE_SSL"           // "true" for an https endpoint
)

// ErrNotFound means the key is not in the bucket.
var ErrNotFound = errors.New("object not found")

// Store is one bucket.
type Store struct {
	client *minio.Client
	bucket string
}

// NewFromEnv builds the store from the environment. It returns nil, and no
// error, when S3_ENDPOINT is not set. It does not connect: a store that is down
// fails the first Put or Get, not the start of the server.
func NewFromEnv() (*Store, error) {
	endpoint := os.Getenv(envEndpoint)
	if endpoint == "" {
		return nil, nil
	}
	keyID, secret := os.Getenv(envKeyID), os.Getenv(envSecret)
	if keyID == "" || secret == "" {
		return nil, fmt.Errorf("%s is set but %s or %s is not", envEndpoint, envKeyID, envSecret)
	}
	bucket := os.Getenv(envBucket)
	if bucket == "" {
		bucket = "maibumai"
	}
	region := os.Getenv(envRegion)
	if region == "" {
		region = "garage"
	}
	useSSL, _ := strconv.ParseBool(os.Getenv(envUseSSL))
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(keyID, secret, ""),
		Secure: useSSL,
		Region: region,
		// Garage answers on one host name; bucket.host would need a DNS entry per bucket
		BucketLookup: minio.BucketLookupPath,
	})
	if err != nil {
		return nil, fmt.Errorf("object store client: %w", err)
	}
	return &Store{client: client, bucket: bucket}, nil
}

// Put stores data under key.
func (s *Store) Put(ctx context.Context, key string, data []byte, contentType string) error {
	_, err := s.client.PutObject(ctx, s.bucket, key, bytes.NewReader(data), int64(len(data)),
		minio.PutObjectOptions{ContentType: contentType})
	return err
}

// Get opens the object under key. The caller closes the reader.
func (s *Store) Get(ctx context.Context, key string) (io.ReadCloser, int64, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, 0, err
	}
	// GetObject is lazy; Stat makes the request, so a missing key shows up here
	info, err := obj.Stat()
	if err != nil {
		_ = obj.Close()
		if minio.ToErrorResponse(err).Code == "NoSuchKey" {
			return nil, 0, ErrNotFound
		}
		return nil, 0, err
	}
	return obj, info.Size, nil
}
