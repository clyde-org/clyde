import numpy as np
import matplotlib.pyplot as plt

# Updated data with latest DragonFly timings
data = {
    'Baseline': {
        'Pod Running': 109.57,
        'import torch': 1.05,
        'import transformers': 0.72,
        'Load Tokenizer': 0.36,
        'Load Model': 0.27,
        'End-to-end': 167.39
    },
    # 'Apull': {
    #     'Pod Running': 16.70,
    #     'import torch': 9.60,
    #     'import transformers': 7.37,
    #     'Load Tokenizer': 1.16,
    #     'Load Model': 86.92,
    #     'End-to-end': 178.97
    # },
    'DragonFly': {
        'Pod Running': 82.84,
        'import torch': 1.02,
        'import transformers': 0.71,
        'Load Tokenizer': 0.36,
        'Load Model': 0.26,
        'End-to-end': 166.38
    },
    'Clyde': {
        'Pod Running': 64.12,
        'import torch': 1.06,
        'import transformers': 0.73,
        'Load Tokenizer': 0.37,
        'Load Model': 0.28,
        'End-to-end': 130.89
    },
    'Clyde+Apull': {
        'Pod Running': 13.67,
        'import torch': 2.73,
        'import transformers': 2.26,
        'Load Tokenizer': 0.43,
        'Load Model': 15.37,
        'End-to-end': 105.45
    }
}

categories = ['Pod Running', 'import torch', 'import transformers', 'Load Tokenizer', 'Load Model']
configs = list(data.keys())
colors = plt.cm.tab10.colors[:len(categories)]

plt.figure(figsize=(12, 7))

# Convert to array format for stacking
values = np.array([[data[config][cat] for cat in categories] for config in configs])

# Plot stacked bars
bottom = np.zeros(len(configs))
for i, category in enumerate(categories):
    plt.bar(configs, values[:, i], bottom=bottom, 
            color=colors[i], label=category)
    bottom += values[:, i]

plt.ylabel('Time (seconds)', fontsize=12)
plt.title('8 Node Cluster Performance (Median Values)', fontsize=14, pad=20)
plt.legend(bbox_to_anchor=(1.05, 1), loc='upper left', fontsize=10)
plt.grid(True, axis='y', linestyle='--', alpha=0.3)

# Add end-to-end time labels above each bar
# for i, config in enumerate(configs):
#     plt.text(i, bottom[i] + 5, f"Total: {data[config]['End-to-end']:.1f}s", 
#              ha='center', va='bottom', fontsize=9)

plt.ylim(0, max(bottom) + 20)  # Adjust ylim to accommodate labels
plt.tight_layout()
plt.savefig('performance_comparison.png', dpi=300, bbox_inches='tight')
plt.show()