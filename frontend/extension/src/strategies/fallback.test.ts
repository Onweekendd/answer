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

import { describe, expect, it } from "vitest";

import { StepFailures, withFallback } from "./fallback";

interface Steps {
  name(n: number): string | undefined;
  list(): (string | null)[];
  flag(): boolean;
}

const generic: Steps = {
  name: (n) => `generic ${n}`,
  list: () => ["generic"],
  flag: () => false
};

describe("withFallback", () => {
  it("没有增强的步骤直接用通用实现", () => {
    // Arrange
    const failures = new StepFailures();

    // Act
    const steps = withFallback<Steps>("demo", { name: (n) => `site ${n}` }, generic, failures);

    // Assert
    expect(steps.name(1)).toBe("site 1");
    expect(steps.list()).toEqual(["generic"]);
    expect(steps.flag).toBe(generic.flag);
    expect(failures.size).toBe(0);
  });

  it("增强返回空就用通用实现：undefined、空数组、全是 null 的数组", () => {
    // Arrange
    const failures = new StepFailures();
    const steps = withFallback<Steps>("demo", { name: () => undefined, list: () => [null] }, generic, failures);

    // Act & Assert
    expect(steps.name(2)).toBe("generic 2");
    expect(steps.list()).toEqual(["generic"]);
    expect(failures.size).toBe(0);
  });

  it("false 和空字符串是有效的结果，不回退", () => {
    // Arrange
    const steps = withFallback<Steps>(
      "demo",
      { flag: () => false, name: () => "" },
      { ...generic, flag: () => true },
      new StepFailures()
    );

    // Act & Assert
    expect(steps.flag()).toBe(false);
    expect(steps.name(1)).toBe("");
  });

  it("增强抛错就用通用实现，并记下是哪一步；同一步只记第一次", () => {
    // Arrange
    const failures = new StepFailures();
    const steps = withFallback<Steps>(
      "demo",
      {
        name: () => {
          throw new Error("broken selector");
        }
      },
      generic,
      failures
    );

    // Act
    const results = [steps.name(1), steps.name(2)];

    // Assert
    expect(results).toEqual(["generic 1", "generic 2"]);
    expect(failures.size).toBe(1);
    expect(failures.toString()).toBe("demo.name: Error: broken selector");
  });

  it("没有任何增强时得到的就是通用实现", () => {
    // Act
    const steps = withFallback<Steps>("demo", undefined, generic, new StepFailures());

    // Assert
    expect(steps.name(3)).toBe("generic 3");
  });
});
