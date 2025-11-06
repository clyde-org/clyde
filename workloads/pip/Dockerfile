FROM python:3.10-slim

WORKDIR /app

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        python3 python3-pip python3-venv python3-dev \
        build-essential git curl ca-certificates \
        libgl1 libglib2.0-0 && \
    rm -rf /var/lib/apt/lists/*

RUN python3 -m pip install --upgrade pip

RUN mkdir -p /data/cache/pip

COPY pip_exp.py /app/pip_exp.py

CMD ["python3", "pip_exp.py"]
