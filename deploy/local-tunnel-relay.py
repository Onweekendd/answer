#!/usr/bin/env python3
# Licensed to the Apache Software Foundation (ASF) under one
# or more contributor license agreements. See the NOTICE file distributed
# with this work for additional information regarding copyright ownership.
# The ASF licenses this file to You under the Apache License, Version 2.0
# (the "License"); you may not use this file except in compliance with
# the License. You may obtain a copy of the License at
# http://www.apache.org/licenses/LICENSE-2.0
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

"""Relay Docker's private gateway to loopback-only reverse SSH forwards.

No SSH daemon setting or public listener is needed. TLS and WebSocket traffic
pass through unchanged; this process never reads application credentials.
"""

import argparse
import asyncio
import ipaddress
from functools import partial

PORTS = {18765: 28765, 13000: 23000, 13443: 23443}


async def copy_stream(reader, writer):
    while data := await reader.read(65536):
        writer.write(data)
        await writer.drain()
    if writer.can_write_eof():
        writer.write_eof()
        await writer.drain()


async def forward(reader, writer, target_port):
    remote_writer = None
    try:
        remote_reader, remote_writer = await asyncio.wait_for(
            asyncio.open_connection("127.0.0.1", target_port), timeout=10
        )
        await asyncio.gather(
            copy_stream(reader, remote_writer), copy_stream(remote_reader, writer)
        )
    except (OSError, asyncio.TimeoutError):
        # No URLs, headers or payloads in logs.
        print(f"tunnel connection unavailable on loopback:{target_port}", flush=True)
    finally:
        for connection in (writer, remote_writer):
            if connection is not None:
                connection.close()
                try:
                    await connection.wait_closed()
                except OSError:
                    pass


async def serve(bind):
    servers = []
    try:
        for listen, target in PORTS.items():
            server = await asyncio.start_server(
                partial(forward, target_port=target), bind, listen
            )
            servers.append(server)
            print(f"private relay {bind}:{listen} -> 127.0.0.1:{target}", flush=True)
        await asyncio.gather(*(server.serve_forever() for server in servers))
    finally:
        for server in servers:
            server.close()
            await server.wait_closed()


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--bind", default="172.17.0.1")
    args = parser.parse_args()
    address = ipaddress.ip_address(args.bind)
    if not address.is_private or address.is_unspecified or address.is_loopback:
        parser.error("--bind must be the private Docker gateway address")
    try:
        asyncio.run(serve(str(address)))
    except KeyboardInterrupt:
        pass
