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
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type S3Config struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	Bucket    string
	Region    string
	Secure    bool
	Prefix    string
	URLTTL    time.Duration
}

type S3Store struct {
	client *minio.Client
	cfg    S3Config
}

func NewS3Store(cfg S3Config) (*S3Store, error) {
	if cfg.Endpoint == "" || cfg.AccessKey == "" || cfg.SecretKey == "" || cfg.Bucket == "" {
		return nil, errors.New("S3 endpoint, credentials and bucket are required")
	}
	if cfg.URLTTL == 0 {
		cfg.URLTTL = time.Hour
	}
	if cfg.URLTTL < time.Minute || cfg.URLTTL > 7*24*time.Hour {
		return nil, errors.New("S3 URL TTL must be between one minute and seven days")
	}
	if cfg.Prefix == "" {
		cfg.Prefix = "assets"
	}
	if strings.HasPrefix(cfg.Prefix, "/") || strings.Contains(cfg.Prefix, "..") || strings.Contains(cfg.Prefix, "\\") {
		return nil, errors.New("invalid S3 prefix")
	}
	client, err := minio.New(cfg.Endpoint, &minio.Options{Creds: credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""), Secure: cfg.Secure, Region: cfg.Region, BucketLookup: minio.BucketLookupPath})
	if err != nil {
		return nil, err
	}
	return &S3Store{client: client, cfg: cfg}, nil
}

// CheckBucket creates only the named bucket when explicitly requested. Existing
// policies and other buckets are never modified.
func (s *S3Store) CheckBucket(ctx context.Context, create bool) error {
	exists, err := s.client.BucketExists(ctx, s.cfg.Bucket)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	if !create {
		return fmt.Errorf("S3 bucket %q does not exist; create it or pass --create-bucket", s.cfg.Bucket)
	}
	return s.client.MakeBucket(ctx, s.cfg.Bucket, minio.MakeBucketOptions{Region: s.cfg.Region})
}

func (s *S3Store) Put(ctx context.Context, scope string, a Asset, data []byte) (Asset, error) {
	parts := strings.Split(scope, "/")
	if len(parts) != 2 || !identifier.MatchString(parts[0]) || !identifier.MatchString(parts[1]) {
		return Asset{}, errors.New("invalid asset scope")
	}
	var ext string
	switch {
	case a.Kind == "video" && a.ContentType == "video/mp4":
		ext = ".mp4"
	case a.Kind == "image" && a.ContentType == "image/jpeg":
		ext = ".jpg"
	default:
		return Asset{}, errors.New("unsupported asset type")
	}
	if len(data) == 0 {
		return Asset{}, errors.New("empty asset")
	}
	hash := sha256.Sum256(data)
	a.SHA256 = hex.EncodeToString(hash[:])
	a.ID = a.SHA256[:24]
	a.Bucket = s.cfg.Bucket
	a.ObjectKey = path.Join(s.cfg.Prefix, scope, a.SHA256+ext)
	a.Bytes = len(data)
	if _, err := s.client.PutObject(ctx, a.Bucket, a.ObjectKey, bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{ContentType: a.ContentType, UserMetadata: map[string]string{"sha256": a.SHA256}}); err != nil {
		return Asset{}, err
	}
	stat, err := s.client.StatObject(ctx, a.Bucket, a.ObjectKey, minio.StatObjectOptions{})
	if err != nil {
		return Asset{}, err
	}
	if stat.Size != int64(len(data)) {
		return Asset{}, errors.New("uploaded object size does not match")
	}
	return s.AccessURL(ctx, a)
}

func (s *S3Store) AccessURL(ctx context.Context, a Asset) (Asset, error) {
	if a.Bucket != s.cfg.Bucket || !strings.HasPrefix(a.ObjectKey, strings.TrimSuffix(s.cfg.Prefix, "/")+"/") {
		return Asset{}, errors.New("asset is outside the configured store")
	}
	u, err := s.client.PresignedGetObject(ctx, a.Bucket, a.ObjectKey, s.cfg.URLTTL, nil)
	if err != nil {
		return Asset{}, err
	}
	a.URL = u.String()
	a.URLExpiresAt = time.Now().UTC().Add(s.cfg.URLTTL)
	return a, nil
}
