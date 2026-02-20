FROM python:3.10-slim

# Set work directory
WORKDIR /app

# Set non-interactive mode
ENV DEBIAN_FRONTEND=noninteractive

# Install system dependencies
RUN apt-get update && apt-get install -y \
    curl \
    wget \
    && rm -rf /var/lib/apt/lists/*

# Install Python packages
RUN pip install --upgrade pip && \
    pip install \
        transformers>=4.51.0 \
        Flask \
        torch \
        accelerate \
        tiktoken \
        einops \
        sentencepiece \
        huggingface_hub

# Copy application and model
COPY ./measure.py /app/measure.py
# COPY ./Qwen--Qwen3-4B-Instruct-2507 /app/Qwen--Qwen3-4B-Instruct-2507
COPY ./DeepSeek-R1-Distill-Llama-8B /app/DeepSeek-R1-Distill-Llama-8B

# Expose port
EXPOSE 5000

# Run app
CMD ["python", "measure.py"]
