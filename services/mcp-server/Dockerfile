# Build from the repository root: docker build -f services/mcp-server/Dockerfile .
FROM python:3.14-slim
ENV PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1 \
    HUNTS_FILE=/app/content/hunting/hunts.yml MCP_TRANSPORT=http
WORKDIR /app
COPY services/mcp-server/pyproject.toml /build/svc/pyproject.toml
COPY services/mcp-server/app /build/svc/app
RUN --mount=type=cache,target=/root/.cache/pip \
    pip install --disable-pip-version-check --root-user-action=ignore /build/svc && rm -rf /build
COPY content/hunting /app/content/hunting
# Fixed uid: the compose certs service gives this user the TLS key; Kubernetes runs as 10001 too
RUN useradd --system --uid 10001 --no-create-home --shell /usr/sbin/nologin app
USER app
EXPOSE 8090
CMD ["python", "-m", "app.main"]
