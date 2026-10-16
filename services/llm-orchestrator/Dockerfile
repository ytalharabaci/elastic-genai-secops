# Build from the repository root: docker build -f services/llm-orchestrator/Dockerfile .
FROM python:3.14-slim
ENV PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1
WORKDIR /app
COPY libs/py-common /build/py-common
COPY services/llm-orchestrator/pyproject.toml /build/svc/pyproject.toml
COPY services/llm-orchestrator/app /build/svc/app
RUN --mount=type=cache,target=/root/.cache/pip     pip install --disable-pip-version-check --root-user-action=ignore /build/py-common /build/svc     && rm -rf /build
RUN useradd --system --no-create-home --shell /usr/sbin/nologin app
USER app
CMD ["python", "-m", "app.main"]
