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

import { useCallback, useEffect, useRef, useState } from 'react';

import isEqual from 'lodash/isEqual';

import type * as Type from '@/common/interface';
import { identifyPurchase } from '@/services';

export type IdentifyStatus = 'idle' | 'loading' | 'done' | 'error';

// Wait this long after the screenshots stop changing. Uploading three one by
// one then costs one identify call instead of three.
const IDENTIFY_DEBOUNCE_MS = 1500;

interface Options {
  images: string[];
  /** The form holds a product: from a draft, the post being edited, or an earlier reading. */
  hasProduct: boolean;
  /** Identify on its own the first time screenshots appear. Off when editing. */
  auto: boolean;
  onIdentified: (product: Type.PurchaseProduct | null) => void;
}

/**
 * When to read the product off the screenshots:
 * - the first time screenshots appear, on its own, once they stop changing;
 * - after that, or once the form holds a product, only when asked (`identify`),
 *   so a later reading never overwrites what the user has already checked.
 *   Changed screenshots are only flagged as `stale`.
 * A request still running when the screenshots change is cancelled; its
 * answer would be for screenshots that are no longer there.
 */
export const usePurchaseIdentify = ({
  images,
  hasProduct,
  auto,
  onIdentified,
}: Options) => {
  const [status, setStatus] = useState<IdentifyStatus>('idle');
  // the screenshots the current product was read from
  const [baseline, setBaseline] = useState<string[] | null>(null);
  const abortRef = useRef<AbortController | null>(null);
  // true once the automatic first reading is no longer wanted
  const settledRef = useRef(false);
  // read the latest callback at answer time, not the one from when the
  // request started: the page's state has moved on during those seconds
  const onIdentifiedRef = useRef(onIdentified);
  onIdentifiedRef.current = onIdentified;

  const run = useCallback((imgs: string[]) => {
    abortRef.current?.abort();
    const controller = new AbortController();
    abortRef.current = controller;
    setStatus('loading');

    identifyPurchase(imgs, controller.signal)
      .then((res) => {
        if (controller.signal.aborted) {
          return;
        }
        settledRef.current = true;
        setBaseline(imgs);
        setStatus('done');
        onIdentifiedRef.current(res?.product ?? null);
      })
      .catch(() => {
        // an abort is ours, not a failure worth showing
        if (controller.signal.aborted) {
          return;
        }
        settledRef.current = true;
        setStatus('error');
      });
  }, []);

  // must run before the auto effect below, so a product loaded with the form
  // (a draft or an edited post) stops the automatic reading in the same commit
  useEffect(() => {
    if (hasProduct) {
      settledRef.current = true;
      // a product loaded with the form was read from the screenshots it came with
      setBaseline((prev) => prev ?? images);
    }
  }, [hasProduct]);

  useEffect(() => {
    if (!auto || settledRef.current) {
      return undefined;
    }
    // the screenshots changed before the first reading came back: that
    // reading is for the old set, so drop it and wait for the new one
    abortRef.current?.abort();
    if (images.length === 0) {
      setStatus('idle');
      return undefined;
    }
    const timer = setTimeout(() => run(images), IDENTIFY_DEBOUNCE_MS);
    return () => clearTimeout(timer);
  }, [images, auto, run]);

  useEffect(() => () => abortRef.current?.abort(), []);

  const identify = useCallback(() => {
    if (images.length > 0) {
      run(images);
    }
  }, [images, run]);

  /** Forget the last reading's status, as when the user rejects it. */
  const reset = useCallback(() => {
    abortRef.current?.abort();
    settledRef.current = true;
    setStatus('idle');
  }, []);

  return {
    status,
    /** the product shown was read from other screenshots than the current ones */
    stale: hasProduct && baseline !== null && !isEqual(baseline, images),
    /** identify now runs only when asked, so offer a way to ask */
    manual: !auto || settledRef.current,
    identify,
    reset,
  };
};
