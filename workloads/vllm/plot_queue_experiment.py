#!/usr/bin/env python3
from __future__ import annotations

import argparse
import csv
from pathlib import Path

import matplotlib.pyplot as plt


def load_per_second(run_dir: Path) -> list[dict]:
    f = run_dir / "per_second.csv"
    if not f.exists():
        raise FileNotFoundError(f"Missing {f}")
    rows: list[dict] = []
    with f.open() as fh:
        reader = csv.DictReader(fh)
        for r in reader:
            rows.append(
                {
                    "second": int(r["second"]),
                    "backlog": int(float(r["backlog"])),
                    "p95_latency_ms": float(r["p95_latency_ms"]),
                }
            )
    return rows


def plot_two_runs(baseline_dir: Path, clyde_dir: Path, outdir: Path) -> None:
    b = load_per_second(baseline_dir)
    c = load_per_second(clyde_dir)
    outdir.mkdir(parents=True, exist_ok=True)

    plt.rcParams.update(
        {
            "font.size": 8,
            "axes.titlesize": 9,
            "axes.labelsize": 8,
            "xtick.labelsize": 7,
            "ytick.labelsize": 7,
            "legend.fontsize": 7,
            "axes.titlepad": 2,
            "axes.labelpad": 2,
        }
    )

    # Backlog over time.
    fig, ax = plt.subplots(figsize=(3.33, 1.9))
    ax.plot([r["second"] for r in b], [r["backlog"] for r in b], label="Baseline", linewidth=1.4, color="#7F3B08")
    ax.plot([r["second"] for r in c], [r["backlog"] for r in c], label="Clyde", linewidth=1.4, color="#E45756")
    ax.set_xlabel("Time (s)")
    ax.set_ylabel("Backlog (req)")
    ax.set_title("Request Backlog Over Time")
    ax.grid(alpha=0.2)
    ax.legend(frameon=False, loc="upper right")
    fig.tight_layout(pad=0.15)
    fig.savefig(outdir / "queue_vs_time.png", dpi=300, bbox_inches="tight", pad_inches=0.02)
    plt.close(fig)

    # p95 latency over time.
    fig, ax = plt.subplots(figsize=(3.33, 1.9))
    ax.plot([r["second"] for r in b], [r["p95_latency_ms"] for r in b], label="Baseline", linewidth=1.4, color="#7F3B08")
    ax.plot([r["second"] for r in c], [r["p95_latency_ms"] for r in c], label="Clyde", linewidth=1.4, color="#E45756")
    ax.set_xlabel("Time (s)")
    ax.set_ylabel("p95 latency (ms)")
    ax.set_title("Latency Over Time (per-second p95)")
    ax.grid(alpha=0.2)
    ax.legend(frameon=False, loc="upper right")
    fig.tight_layout(pad=0.15)
    fig.savefig(outdir / "latency_vs_time.png", dpi=300, bbox_inches="tight", pad_inches=0.02)
    plt.close(fig)

    # Compact summary CSV.
    def agg(rows: list[dict]) -> tuple[int, float, float]:
        if not rows:
            return (0, 0.0, 0.0)
        peak = max(r["backlog"] for r in rows)
        auc = sum(r["backlog"] for r in rows)
        mean_p95 = sum(r["p95_latency_ms"] for r in rows) / len(rows)
        return peak, float(auc), float(mean_p95)

    b_peak, b_auc, b_mean_p95 = agg(b)
    c_peak, c_auc, c_mean_p95 = agg(c)
    with (outdir / "queue_latency_summary.csv").open("w", newline="") as fh:
        w = csv.writer(fh)
        w.writerow(["scenario", "peak_backlog", "backlog_auc", "mean_p95_ms"])
        w.writerow(["baseline", b_peak, f"{b_auc:.3f}", f"{b_mean_p95:.3f}"])
        w.writerow(["clyde", c_peak, f"{c_auc:.3f}", f"{c_mean_p95:.3f}"])


def main() -> None:
    p = argparse.ArgumentParser(description="Plot queue/latency time-series from baseline and clyde runs.")
    p.add_argument("--baseline-dir", type=Path, required=True)
    p.add_argument("--clyde-dir", type=Path, required=True)
    p.add_argument("--outdir", type=Path, default=Path("workloads/vllm/results/plots"))
    args = p.parse_args()
    plot_two_runs(args.baseline_dir, args.clyde_dir, args.outdir)
    print(f"Wrote plots and summary to: {args.outdir}")


if __name__ == "__main__":
    main()
