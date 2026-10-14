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

import request from '@/utils/request';
import type * as Type from '@/common/interface';

// one vision call takes around ten seconds and a malformed reply is retried
// once, so the default 10s timeout would cut most of them off
const IDENTIFY_TIMEOUT_MS = 60000;

/**
 * Reads the product off just-uploaded screenshots. A failure is left to the
 * caller to show quietly (`ignoreError`): the form works without a reading,
 * so it must not take the page over with the global error screen.
 */
export const votePurchase = (
  questionId: string,
  choice: Type.PurchaseVoteChoice,
) => {
  return request.post<Type.PurchaseVoteResp>('/answer/api/v1/purchase/vote', {
    question_id: questionId,
    choice,
  });
};

export const identifyPurchase = (images: string[], signal?: AbortSignal) => {
  return request.post<Type.PurchaseIdentifyResp>(
    '/answer/api/v1/purchase/ai/identify',
    { images },
    { signal, timeout: IDENTIFY_TIMEOUT_MS, ignoreError: '50X' },
  );
};
