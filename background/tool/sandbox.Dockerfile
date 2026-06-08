FROM python:3.11-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    bash \
    ca-certificates \
    curl \
    fonts-dejavu-core \
    gcc \
    libffi-dev \
    libjpeg-dev \
    libxml2-dev \
    libxslt1-dev \
    pandoc \
    && rm -rf /var/lib/apt/lists/*

RUN pip install --no-cache-dir \
    cairosvg \
    lxml \
    Pillow \
    python-pptx

WORKDIR /workspace

CMD ["sleep", "infinity"]
