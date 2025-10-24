FROM ubuntu:20.04

# Set work directory
WORKDIR /app

# Set proxy
ENV http_proxy=http://172.17.0.1:3128
ENV https_proxy=http://172.17.0.1:3128
ENV no_proxy=localhost,127.0.0.1

# Set non-interactive mode (for tzdata etc.)
ENV DEBIAN_FRONTEND=noninteractive

# Install system packages & Python 3.8
RUN apt-get update && apt-get install -y \
    software-properties-common \
    curl \
    wget \
    vim \
    gcc \
    g++ \
    ca-certificates \
    gnupg \
    procps \
    python3.8 \
    python3.8-distutils \
    python3-pip

# Use python3.8 as default
RUN update-alternatives --install /usr/bin/python python /usr/bin/python3.8 1

# Trust Huawei CA certificates
COPY certs/HuaweiITRootCA.pem /usr/local/share/ca-certificates/HuaweiITRootCA.crt
COPY certs/HuaweiItEnterpriseCA1.pem /usr/local/share/ca-certificates/HuaweiItEnterpriseCA1.crt
COPY certs/huawei.pem /usr/local/share/ca-certificates/huawei.crt
COPY certs/HuaweiSecureInternetProxyCA.pem /usr/local/share/ca-certificates/HuaweiSecureInternetProxyCA.crt
COPY certs/HuaweiWebSecureInternetGatewayCA.pem /usr/local/share/ca-certificates/HuaweiWebSecureInternetGatewayCA.crt
RUN update-ca-certificates

# Upgrade pip and install Python packages
# Upgrade pip and install Python packages
RUN curl -sS https://bootstrap.pypa.io/pip/3.8/get-pip.py | python && \
    pip install --upgrade pip && \
    pip install \
        transformers \
        Flask \
        torch \
        flax \
        sentencepiece \
        huggingface_hub \
        accelerate

RUN pip uninstall jax -y

# Copy application and model
COPY ./run_model.py /app/run_model.py
COPY ./measure.py /app/measure.py
COPY ./Llama-3.2-1B /app/Llama-3.2-1B

# Expose port
EXPOSE 5000

# Run app
CMD ["python", "measure.py"]
