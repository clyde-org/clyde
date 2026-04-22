#!/usr/bin/env python3
"""Regenerate docs/img/cold-start-chart.png and workloads/cold-start/cold-start-analysis.pdf."""

from __future__ import annotations

from pathlib import Path

import matplotlib.pyplot as plt
from fpdf import FPDF
from matplotlib.patches import Patch

HERE = Path(__file__).resolve().parent
# Chart is versioned under docs/ for install guide; PDF stays local to this workload.
CHART_PNG = HERE.parent.parent / "docs" / "img" / "cold-start-chart.png"

# TensorFlow ~1 GB: two cold pulls in capture — slower run = Baseline, faster = Clyde (labels only).
TF_BASELINE = [
    54.379,
    78.704,
    78.751,
    80.571,
    80.904,
    87.388,
    90.060,
    90.073,
    92.432,
    92.543,
    92.978,
    95.411,
    97.156,
]
TF_CLYDE = [
    20.763,
    39.389,
    40.545,
    44.923,
    62.995,
    63.025,
    67.093,
    69.989,
    71.661,
    75.116,
    75.248,
    75.254,
    84.387,
]

VLLM_BASELINE = [
    367.698,
    372.963,
    558.782,
    660.765,
    663.343,
    813.326,
    831.998,
    842.918,
    855.786,
    878.775,
    884.294,
    894.457,
    916.718,
]
VLLM_CLYDE = [
    139.115,
    145.037,
    168.128,
    171.939,
    180.067,
    183.515,
    183.823,
    200.854,
    204.173,
    214.103,
    250.361,
    260.714,
    313.283,
]

COL_BASELINE = "#C44E52"
COL_CLYDE = "#55A868"


def _boxpair(ax, baseline, clyde, title: str) -> None:
    bp = ax.boxplot(
        [baseline, clyde],
        positions=[1, 2],
        tick_labels=["Baseline", "Clyde"],
        patch_artist=True,
        showmeans=True,
        meanline=True,
        widths=0.55,
    )
    for patch, color in zip(bp["boxes"], [COL_BASELINE, COL_CLYDE], strict=True):
        patch.set_facecolor(color)
        patch.set_alpha(0.5)
    ax.set_ylabel("seconds")
    # Smaller pad keeps subplot titles lower (large pad moves them up toward fig.suptitle).
    ax.set_title(title, fontsize=10, pad=4)
    ax.tick_params(axis="both", labelsize=9)


def write_chart_png(path: Path) -> None:
    try:
        plt.style.use("seaborn-v0_8-whitegrid")
    except OSError:
        plt.style.use("ggplot")

    # Square figure (equal width and height in inches).
    side = 6.0
    fig, axes = plt.subplots(1, 2, figsize=(side, side), layout="constrained")
    # rect (l, b, r, t): suptitle, panel titles, outside legend.
    fig.get_layout_engine().set(rect=(0.06, 0.10, 0.94, 0.78))

    _boxpair(axes[0], VLLM_BASELINE, VLLM_CLYDE, "vLLM Ascend (~5.6 GB)")
    _boxpair(axes[1], TF_BASELINE, TF_CLYDE, "TensorFlow (~1 GB)")

    handles = [
        Patch(
            facecolor=COL_BASELINE,
            edgecolor="#222222",
            linewidth=0.5,
            alpha=0.5,
            label="Baseline",
        ),
        Patch(
            facecolor=COL_CLYDE,
            edgecolor="#222222",
            linewidth=0.5,
            alpha=0.5,
            label="Clyde",
        ),
    ]
    fig.legend(
        handles=handles,
        loc="outside lower center",
        ncol=2,
        frameon=True,
        fontsize=9,
    )

    fig.suptitle(
        "Cold start pull time (13 nodes, no cache / no seed)",
        fontsize=11,
        y=0.99,
    )

    fig.savefig(path, dpi=150, bbox_inches="tight")
    plt.close(fig)


def write_pdf(chart_png: Path, path: Path) -> None:
    pdf = FPDF()
    pdf.set_margins(left=12, top=12, right=12)
    pdf.set_auto_page_break(auto=True, margin=14)
    pdf.add_page()
    text_w = pdf.w - pdf.l_margin - pdf.r_margin

    def bullet(txt: str, h: float = 5.2) -> None:
        pdf.set_x(pdf.l_margin)
        pdf.multi_cell(text_w, h, txt)

    pdf.set_font("Helvetica", "B", 14)
    bullet("Cold start: Baseline vs Clyde", h=7)
    pdf.ln(0.5)

    pdf.set_font("Helvetica", size=10)
    bullet("- 13-node DaemonSet, one cold pull per node; Baseline left, Clyde right on every plot.")
    bullet(
        "- Clyde follows containerd /content/create (blobs advertise incrementally); "
        "Baseline tracks /images/create and full-image indexing (containerd.go ~275, ~356, ~382)."
    )
    pdf.ln(1)

    pdf.image(str(chart_png), x=pdf.l_margin, w=text_w)

    pdf.output(path)


def main() -> None:
    chart = CHART_PNG
    chart.parent.mkdir(parents=True, exist_ok=True)
    pdf_out = HERE / "cold-start-analysis.pdf"
    write_chart_png(chart)
    write_pdf(chart, pdf_out)
    print(f"Wrote {chart}")
    print(f"Wrote {pdf_out}")


if __name__ == "__main__":
    main()
