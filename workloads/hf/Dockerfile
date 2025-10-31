FROM python:3.10-slim

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

CMD ["python3", "hf_exp.py"]
