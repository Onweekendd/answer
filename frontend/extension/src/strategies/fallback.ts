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

// 一项能力的步骤：一组同步的纯函数。写成映射类型而不是 Record<string, …>：接口没有索引签名，满足不了后者
type Steps<S> = { [K in keyof S]: (...args: never[]) => unknown };

// StepFailures 记下哪一步的增强抛了错。同一步只记第一次：label 对每个元素都调一次，
// 选择器失效时会对每个元素都抛同样的错
export class StepFailures {
  private readonly byStep = new Map<string, string>();

  record(step: string, err: unknown): void {
    if (!this.byStep.has(step)) {
      this.byStep.set(step, String(err));
    }
  }

  get size(): number {
    return this.byStep.size;
  }

  // "snapshot.candidates: Error: broken selector; list.title: …"
  toString(): string {
    return Array.from(this.byStep, ([step, err]) => `${step}: ${err}`).join("; ");
  }
}

// withFallback 把页面的增强和通用实现合成一套完整的步骤：每一步先用增强，
// 增强没写、抛错、或者返回空（undefined、null、空数组、全是 null 的数组），就用通用实现。
// capability 只用来给失败记录起名字（"snapshot.label"）
export function withFallback<S extends Steps<S>>(
  capability: string,
  enhanced: Partial<S> | undefined,
  generic: S,
  failures: StepFailures
): S {
  const merged = {} as Record<string, unknown>;
  for (const [step, fallback] of Object.entries(generic) as [string, (...args: never[]) => unknown][]) {
    const own = enhanced?.[step as keyof S];
    if (typeof own !== "function") {
      merged[step] = fallback;
      continue;
    }
    merged[step] = (...args: never[]) => {
      try {
        const value = (own as (...a: never[]) => unknown)(...args);
        if (!isEmpty(value)) {
          return value;
        }
      } catch (err) {
        failures.record(`${capability}.${step}`, err);
      }
      return fallback(...args);
    };
  }
  return merged as S;
}

function isEmpty(value: unknown): boolean {
  if (value === undefined || value === null) {
    return true;
  }
  return Array.isArray(value) && value.every((v) => v === undefined || v === null);
}
