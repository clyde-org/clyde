#!/usr/bin/env python3
"""
Fixed-rate HTTP load runner with per-request and per-second queue metrics.

Outputs:
- requests.csv: one row per request
- per_second.csv: sent/completed/backlog and p95 latency per second
"""

from __future__ import annotations

import argparse
import csv
import math
import threading
import time
import urllib.error
import urllib.request
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path


def run_request(req_id: int, endpoint: str, path: str, timeout: float) -> dict:
    sent_ts = time.time()
    status = 0
    ok = 0
    err = ""
    try:
        req = urllib.request.Request(f"{endpoint}{path}", method="GET")
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            status = int(resp.status)
            _ = resp.read(1024)
            ok = 1 if 200 <= status < 400 else 0
    except urllib.error.HTTPError as e:
        status = int(e.code)
        err = str(e)
    except Exception as e:  # noqa: BLE001
        err = str(e)
    done_ts = time.time()
    lat_ms = (done_ts - sent_ts) * 1000.0
    return {
        "request_id": req_id,
        "sent_ts": sent_ts,
        "done_ts": done_ts,
        "latency_ms": lat_ms,
        "status": status,
        "ok": ok,
        "error": err,
    }


def percentile(values: list[float], p: float) -> float:
    if not values:
        return 0.0
    arr = sorted(values)
    if len(arr) == 1:
        return arr[0]
    pos = (p / 100.0) * (len(arr) - 1)
    lo = int(math.floor(pos))
    hi = int(math.ceil(pos))
    if lo == hi:
        return arr[lo]
    frac = pos - lo
    return arr[lo] + frac * (arr[hi] - arr[lo])


def main() -> None:
    parser = argparse.ArgumentParser(description="Run fixed-rate load and export queue metrics.")
    parser.add_argument("--endpoint", required=True, help="Endpoint base URL, e.g. http://lb-host")
    parser.add_argument("--path", default="/v1/models", help="Request path (default: /v1/models)")
    parser.add_argument("--duration-sec", type=int, default=300, help="Load duration seconds")
    parser.add_argument("--rps", type=float, default=3.0, help="Constant requests per second")
    parser.add_argument("--concurrency", type=int, default=32, help="Thread pool worker count")
    parser.add_argument("--timeout-sec", type=float, default=20.0, help="Request timeout seconds")
    parser.add_argument("--output-dir", required=True, help="Directory for CSV outputs")
    args = parser.parse_args()

    out = Path(args.output_dir)
    out.mkdir(parents=True, exist_ok=True)
    req_csv = out / "requests.csv"
    per_sec_csv = out / "per_second.csv"

    results: list[dict] = []
    lock = threading.Lock()

    start = time.time()
    end = start + args.duration_sec
    interval = 1.0 / args.rps if args.rps > 0 else 1.0
    next_send = start
    req_id = 0

    with ThreadPoolExecutor(max_workers=args.concurrency) as pool:
        futures = []
        while True:
            now = time.time()
            if now >= end:
                break
            if now < next_send:
                time.sleep(min(0.01, next_send - now))
                continue
            futures.append(pool.submit(run_request, req_id, args.endpoint, args.path, args.timeout_sec))
            req_id += 1
            next_send += interval

        for fut in futures:
            row = fut.result()
            with lock:
                results.append(row)

    if not results:
        raise SystemExit("No requests were sent; check duration/rps inputs.")

    results.sort(key=lambda x: x["request_id"])
    with req_csv.open("w", newline="") as f:
        w = csv.DictWriter(
            f,
            fieldnames=["request_id", "sent_ts", "done_ts", "latency_ms", "status", "ok", "error"],
        )
        w.writeheader()
        for row in results:
            w.writerow(row)

    # Build per-second queue stats.
    s0 = min(r["sent_ts"] for r in results)
    last_done = max(r["done_ts"] for r in results)
    total_secs = int(math.ceil(last_done - s0)) + 1

    sent = [0] * total_secs
    done = [0] * total_secs
    lats: list[list[float]] = [[] for _ in range(total_secs)]

    for r in results:
        si = max(0, min(total_secs - 1, int(r["sent_ts"] - s0)))
        di = max(0, min(total_secs - 1, int(r["done_ts"] - s0)))
        sent[si] += 1
        done[di] += 1
        lats[di].append(float(r["latency_ms"]))

    cum_sent = 0
    cum_done = 0
    with per_sec_csv.open("w", newline="") as f:
        w = csv.writer(f)
        w.writerow(
            [
                "second",
                "sent",
                "completed",
                "cum_sent",
                "cum_completed",
                "backlog",
                "p95_latency_ms",
            ]
        )
        for sec in range(total_secs):
            cum_sent += sent[sec]
            cum_done += done[sec]
            backlog = max(0, cum_sent - cum_done)
            p95 = percentile(lats[sec], 95.0)
            w.writerow([sec, sent[sec], done[sec], cum_sent, cum_done, backlog, f"{p95:.3f}"])

    print(f"Wrote {req_csv}")
    print(f"Wrote {per_sec_csv}")


if __name__ == "__main__":
    main()
