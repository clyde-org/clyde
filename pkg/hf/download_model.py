import os
# Proxy and cache settings
os.environ["HF_HUB_CACHE"] = "/data/cache/hf/model"
os.environ["HF_HUB_VERBOSITY"] = "debug"  # optional, more logs

# If your proxy mirrors HuggingFace, you can also override the endpoint:
os.environ["HF_ENDPOINT"] = "http://7.151.6.248:30020/huggingface"


from huggingface_hub import hf_hub_download


# Test download
hf_hub_download(
    repo_id="tiiuae/falcon-7b",
    filename="config.json",
    force_download=False,   # don’t re-download if cached
)
