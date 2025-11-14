import matplotlib.pyplot as plt
import numpy as np

# Data
# categories = [
#     'Reach Running',
#     'Import torch', 
#     'Import transformers', 
#     'Load Tokenizer', 
#     'Load Model',
#     'End-to-End'  # New column for total time
# ]

# baseline = [91.53, 1.04, 0.73, 0.36, 0.28, 91.53+1.04+0.73+0.36+0.28]
# apull = [12.17, 9.77, 7.36, 1.17, 80.61, 12.17+9.77+7.36+1.17+80.61]
# clyde = [64.26, 1.02, 0.71, 0.36, 0.27, 64.26+1.02+0.71+0.36+0.27]
# clyde_apull = [12.13, 2.64, 2.22, 0.42, 13.11, 12.13+2.64+2.22+0.42+13.11]

# x = np.arange(len(categories))
# width = 0.18

# plt.figure(figsize=(12, 6))

# # Create bars
# bars1 = plt.bar(x - 1.5*width, baseline, width, label='Baseline')
# bars2 = plt.bar(x - 0.5*width, apull, width, label='Apull')
# bars3 = plt.bar(x + 0.5*width, clyde, width, label='Clyde')
# bars4 = plt.bar(x + 1.5*width, clyde_apull, width, label='Clyde+Apull')

# plt.xlabel('Operations')
# plt.ylabel('Time (seconds)')
# plt.title('Container Initialization Performance Comparison')
# plt.xticks(x, categories)
# plt.legend(bbox_to_anchor=(1.02, 1), loc='upper left')
# plt.grid(True, axis='y', linestyle='--', alpha=0.7)

# # Add horizontal line to separate the sum column
# plt.axvline(x=4.5, color='gray', linestyle='--', linewidth=1)

# plt.tight_layout()
# plt.savefig('plot.png')



# Data in chronological order
data = {
    'Baseline': {'Pod Running': 91.53, 'import torch': 1.04, 
                'import transformers': 0.73, 'Load Tokenizer': 0.36, 
                'Load Model': 0.28},
    'Apull': {'Pod Running': 12.17, 'import torch': 9.77, 
             'import transformers': 7.36, 'Load Tokenizer': 1.17, 
             'Load Model': 80.61},
    'Clyde': {'Pod Running': 64.26, 'import torch': 1.02, 
             'import transformers': 0.71, 'Load Tokenizer': 0.36, 
             'Load Model': 0.27},
    'Clyde+Apull': {'Pod Running': 12.13, 'import torch': 2.64, 
                   'import transformers': 2.22, 'Load Tokenizer': 0.42, 
                   'Load Model': 13.11}
}

categories = ['Pod Running', 'import torch', 'import transformers', 
              'Load Tokenizer', 'Load Model']
configs = list(data.keys())
colors = plt.cm.tab10.colors[:5]  # Using a colormap for distinct colors

plt.figure(figsize=(10, 6))

# Convert to array format for stacking
values = np.array([[data[config][cat] for cat in categories] for config in configs])

# Plot stacked bars
bottom = np.zeros(len(configs))
for i, category in enumerate(categories):
    plt.bar(configs, values[:, i], bottom=bottom, 
            color=colors[i], label=category)
    bottom += values[:, i]

# Add total time labels
totals = bottom
for i, total in enumerate(totals):
    plt.text(i, total + 2, f'{total:.1f}s', ha='center', va='bottom')

plt.ylabel('Time (seconds)')
plt.title('Clyde, Apull and Baseline Performance')
plt.legend(bbox_to_anchor=(1.05, 1), loc='upper left')
# plt.grid(True, axis='y', linestyle='--', alpha=0.7)
plt.tight_layout()
plt.savefig('stack.png')