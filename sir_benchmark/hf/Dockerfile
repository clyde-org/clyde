FROM ubuntu:20.04

ENV DEBIAN_FRONTEND=noninteractive

WORKDIR /app

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        python3 \
        python3-pip \
        python3-venv \
        git \
        curl \
        ca-certificates && \
    rm -rf /var/lib/apt/lists/*

RUN python3 -m pip install --upgrade pip

RUN pip install huggingface_hub

RUN mkdir -p /data/cache/hf/model

COPY hf_exp.py /app/hf_exp.py

ENV HF_HUB_CACHE="/data/cache/hf/model"
ENV HF_HUB_DISABLE_XET="1"
ENV HF_ENDPOINT="http://127.0.0.1:30020/huggingface"

CMD ["python3", "/app/hf_exp.py"]
