# Поисковый движок DocFinder. Внешние запросы выполняются только к API модели из настроек.
FROM python:3.12.10-slim

ENV PYTHONDONTWRITEBYTECODE=1 \
	PYTHONUNBUFFERED=1 \
	PYTHONIOENCODING=utf-8 \
	ENGINE_HOST=0.0.0.0 \
	ENGINE_PORT=8000 \
	ENGINE_KB_PATH=/kb \
	ENGINE_CACHE_DIR=/cache

WORKDIR /app
COPY requirements.txt ./
RUN pip install --no-cache-dir -r requirements.txt

COPY *.py ./

RUN useradd --system --uid 10001 engine && mkdir -p /cache && chown engine /cache
USER engine

EXPOSE 8000
HEALTHCHECK --interval=10s --timeout=5s --start-period=10s --retries=6 \
	CMD python -c "import urllib.request,sys; sys.exit(0 if urllib.request.urlopen('http://127.0.0.1:8000/health', timeout=4).status == 200 else 1)"

CMD ["python", "server.py"]
