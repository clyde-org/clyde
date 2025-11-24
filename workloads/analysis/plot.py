import pandas as pd
import matplotlib.pyplot as plt
from io import StringIO

# Raw CSV data
pip_csv = """baseline,clyde
1299.32,292.18
1276.84,208.62
1246.12,204.31
1272.65,293.78
1258.5,313.29
1260.67,317.12
1265.72,317.35
1300.52,309.29
1285.65,210.34
1274.76,315.56
1299.21,320.51
1254.79,293.52
1253.95,157.16
1291.27,308.27
1302.28,310.74
1259.74,311.86
1265.01,290.75
1281.73,296.56
1246.44,317.58
1270.93,311.23
1271.11,312.33
1274.63,318.2
1281.77,322.11
1258.49,293.65
1271.07,290.91
1295.99,293.76
1286.46,311.08
"""

container_csv = """baseline,clyde
6229.887,397.89
6334.978,403.617
6393.413,414.865
6439.569,418.716
6440.478,431.021
6463.874,458.511
6484.652,461.968
6545.576,787.39
6572.995,788.197
6604.563,802.63
6628.362,802.747
6651.495,804.121
6699.648,804.851
6709.571,804.91
6726.501,806.202
6733.334,806.401
6756.484,808.391
6760.693,808.789
6809.518,838.644
6876.714,846.766
6893.987,853.329
"""

huggingface_csv = """baseline,clyde
16597.05,2239.42
15894.55,2256.17
16247.43,2229.46
16482.33,2229.60
16517.82,2227.51
15785.41,2224.40
15465.81,0.49
15616.78,2217.91
16027.72,2226.81
15968.65,2226.42
15271.37,2255.58
16159.63,2222.95
16560.23,2194.98
15887.84,0.39
16203.92,2226.84
16448.59,2222.61
15559.50,0.41
16070.32,2226.66
15878.62,2243.99
16584.48,2228.54
15786.58,1519.77
15859.68,2228.47
16340.51,2207.84
16446.84,2224.75
15880.12,2228.27
16351.61,0.42
15897.18,2228.27
"""

# Experiment mapping
experiments = [
    {
        "data": pip_csv, 
        "title": "Pip Package Installation", 
        "extra": "",
    },
    {
        "data": container_csv, 
        "title": "Container Image Download", 
        "extra": "Image size: 18.6GB, Model: DeepSeek-R1-Distill-Llama-8B"
    },
    {
        "data": huggingface_csv, 
        "title": "HuggingFace Model Download", 
        "extra": "Model: DeepSeek-R1-Distill-Qwen-32B, Size: 65.5GB"
    }
]

# Generate and save plots
for exp in experiments:
    df = pd.read_csv(StringIO(exp["data"]))
    
    # Convert seconds to minutes
    df_min = df / 60
    
    # Calculate average
    avg_values = df_min.mean()
    
    # Performance improvement
    improvement = avg_values['baseline'] / avg_values['clyde']
    
    # Plot
    plt.figure(figsize=(7,4))
    avg_values.plot(kind='bar', color=['skyblue', 'orange'])
    plt.xticks(rotation=0)
    plt.ylabel("Average Time (minutes)")
    
    plt.title(f"{exp['title']} - Performance Improved x{improvement:.2f}\n{exp['extra']}", fontsize=10)
    plt.tight_layout()
    
    # Save
    output_file = exp["title"].replace(" ", "_").lower() + ".png"
    plt.savefig(output_file)
    plt.close()
    print(f"Saved plot: {output_file}")
