import pandas as pd
import matplotlib.pyplot as plt

# Data
data = {
    "Typ": ["clyde"]*21 + ["baseline"]*21,
    "Pull_Time": [
        "6m37.89s","6m43.617s","6m54.865s","6m58.716s","7m11.021s","7m38.511s","7m41.968s",
        "13m7.39s","13m8.197s","13m22.63s","13m22.747s","13m24.121s","13m24.851s","13m24.91s",
        "13m26.202s","13m26.401s","13m28.391s","13m28.789s","13m58.644s","14m6.766s","14m13.329s",
        "1h43m39.887s","1h45m24.978s","1h45m33.413s","1h46m19.569s","1h47m20.478s","1h47m43.874s",
        "1h48m4.652s","1h49m5.576s","1h49m32.995s","1h50m4.563s","1h50m28.362s","1h50m51.495s",
        "1h51m39.648s","1h51m49.571s","1h52m6.501s","1h52m13.334s","1h52m36.484s","1h52m40.693s",
        "1h53m29.518s","1h54m36.714s","1h55m13.987s"
    ]
}

# Create DataFrame
df = pd.DataFrame(data)

# Function to convert Pull_Time to minutes
def time_to_minutes(t):
    if 'h' in t:
        h, rest = t.split('h')
        m, s = rest.split('m')
        s = s.replace('s','')
        return int(h)*60 + int(m) + float(s)/60
    else:
        m, s = t.split('m')
        s = s.replace('s','')
        return int(m) + float(s)/60

df['Time_Min'] = df['Pull_Time'].apply(time_to_minutes)

# Average per type
avg_df = df.groupby('Typ')['Time_Min'].mean().reset_index()

# Plot
plt.figure(figsize=(6,4))
plt.bar(avg_df['Typ'], avg_df['Time_Min'], color=['skyblue','salmon'])
plt.ylabel('Average Pull Time (minutes)')
plt.title('Average Pull Time: Clyde vs Baseline')
for index, row in avg_df.iterrows():
    plt.text(row.name, row.Time_Min + 1, f"{row.Time_Min:.2f}", ha='center')
plt.show()
