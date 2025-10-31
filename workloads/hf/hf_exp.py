import os
import socket
import time
from datetime import datetime

# Get current node IP dynamically
def get_local_ip():
    s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    try:
        s.connect(("8.8.8.8", 80))
        ip = s.getsockname()[0]
    except Exception:
        ip = "127.0.0.1"
    finally:
        s.close()
    return ip

# Proxy and cache settings
LOCAL_IP = get_local_ip()
os.environ["HF_HUB_CACHE"] = "/data/cache/hf/model"
os.environ["HF_HUB_DISABLE_XET"] = "1"
os.environ["HF_ENDPOINT"] = f"http://{LOCAL_IP}:30020/huggingface"

## Import after env vars are set
from huggingface_hub import snapshot_download

# hf_token = "REPLACE_WITH_YOUR_HF_KEY"
repo_id = "deepseek-ai/DeepSeek-R1-Distill-Qwen-32B"

# Start time outside the loop
start = time.time()

# Simple retry - just run it multiple times
for i in range(10):
    try:
        print(f"Download attempt {i+1}...")
        snapshot_download(repo_id=repo_id)
        end = time.time()
        print(f"Download completed in {end - start:.2f} seconds")
        break
    except Exception as e:
        print(f"Attempt {i+1} failed: {e}")
        if i < 9:  # Only retry if not the last attempt
            print("Retrying in 5 seconds...")
            time.sleep(5)
        else:
            end = time.time()
            print(f"All attempts failed after {end - start:.2f} seconds")


# Print completion message with current time
now = datetime.now().strftime("%H:%M")
print(f"Experiment completed at {now}")

# Keep container alive forever
while True:
    time.sleep(3600)  # sleep 1 hour to avoid CPU spin