import os
import time
from datetime import datetime


os.environ["HF_HUB_CACHE"] = "/data/cache/hf/model"
os.environ["HF_HUB_DISABLE_XET"] = "1"
os.environ["HF_HUB_DOWNLOAD_TIMEOUT"] = "120" #Bluezone p2p can be very slow

# Only set HF_ENDPOINT if USE_LOCAL_PROXY=true
if os.environ.get("USE_LOCAL_PROXY", "false").lower() == "true":
    node_ip = os.environ.get("NODE_IP", "127.0.0.1")
    os.environ["HF_ENDPOINT"] = f"http://{node_ip}:30020/huggingface"

from huggingface_hub import snapshot_download

repo_id = "deepseek-ai/DeepSeek-R1-Distill-Qwen-32B"
start = time.time()

for i in range(10):
    try:
        print(f"Download attempt {i+1}...")
        snapshot_download(repo_id=repo_id, force_download=True)
        end = time.time()
        print(f"Download completed in {end - start:.2f} seconds")
        break
    except Exception as e:
        print(f"Attempt {i+1} failed: {e}")
        if i < 9:
            print("Retrying in 5 seconds...")
            time.sleep(5)
        else:
            end = time.time()
            print(f"All attempts failed after {end - start:.2f} seconds")

now = datetime.now().strftime("%H:%M")
print(f"Experiment completed at {now}")

while True:
    time.sleep(3600)
