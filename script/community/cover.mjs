/* Licensed to the Apache Software Foundation (ASF) under one or more
 * contributor license agreements. See the NOTICE file distributed with this
 * work for additional information regarding copyright ownership.
 * The ASF licenses this file to You under the Apache License, Version 2.0
 * (the "License"); you may not use this file except in compliance with
 * the License. You may obtain a copy of the License at
 * http://www.apache.org/licenses/LICENSE-2.0
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

import { createHash } from "node:crypto";
import { deflateSync } from "node:zlib";

const palettes = [
  [
    [239, 231, 213],
    [44, 73, 58],
    [203, 177, 128],
  ],
  [
    [231, 238, 251],
    [44, 66, 126],
    [181, 197, 232],
  ],
  [
    [250, 228, 230],
    [143, 52, 76],
    [226, 170, 183],
  ],
  [
    [236, 231, 248],
    [85, 61, 129],
    [196, 179, 224],
  ],
  [
    [239, 240, 223],
    [78, 88, 46],
    [195, 202, 153],
  ],
];

function crc32(data) {
  let crc = 0xffffffff;
  for (const byte of data) {
    crc ^= byte;
    for (let bit = 0; bit < 8; bit++)
      crc = (crc >>> 1) ^ (crc & 1 ? 0xedb88320 : 0);
  }
  return (crc ^ 0xffffffff) >>> 0;
}

function chunk(type, data) {
  const name = Buffer.from(type);
  const length = Buffer.alloc(4);
  length.writeUInt32BE(data.length);
  const crc = Buffer.alloc(4);
  crc.writeUInt32BE(crc32(Buffer.concat([name, data])));
  return Buffer.concat([length, name, data, crc]);
}

/** A local abstract discussion cover, never a fabricated product photo. No packages needed. */
export function makeCover(seed) {
  const hash = createHash("sha256").update(seed).digest();
  const [background, ink, accent] = palettes[hash[0] % palettes.length];
  const width = 480;
  const height = 580 + (hash[1] % 3) * 40;
  const pixels = Buffer.alloc((width * 3 + 1) * height);
  function dot(x, y, radius, color) {
    for (
      let py = Math.max(0, Math.floor(y - radius));
      py < Math.min(height, y + radius);
      py++
    ) {
      for (
        let px = Math.max(0, Math.floor(x - radius));
        px < Math.min(width, x + radius);
        px++
      ) {
        if ((px - x) ** 2 + (py - y) ** 2 <= radius ** 2) {
          const i = py * (width * 3 + 1) + 1 + px * 3;
          pixels.set(color, i);
        }
      }
    }
  }
  for (let y = 0; y < height; y++) {
    for (let x = 0; x < width; x++)
      pixels.set(background, y * (width * 3 + 1) + 1 + x * 3);
  }
  dot(450, 30, 145, accent);
  dot(15, height - 10, 120, accent);
  // A large round question mark, rendered as a stroke and a dot.
  for (let t = -Math.PI; t < Math.PI / 2; t += 0.008) {
    dot(240 + 83 * Math.cos(t), 225 + 83 * Math.sin(t), 19, ink);
  }
  for (let y = 308; y < 350; y++) dot(240, y, 19, ink);
  dot(240, 408, 23, ink);
  for (let x = 165; x < 315; x++) dot(x, height - 65, 2, ink);
  const header = Buffer.alloc(13);
  header.writeUInt32BE(width, 0);
  header.writeUInt32BE(height, 4);
  header[8] = 8;
  header[9] = 2;
  return Buffer.concat([
    Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]),
    chunk("IHDR", header),
    chunk("IDAT", deflateSync(pixels)),
    chunk("IEND", Buffer.alloc(0)),
  ]);
}
