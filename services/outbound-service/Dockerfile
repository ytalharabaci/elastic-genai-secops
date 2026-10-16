# Build from the repository root: docker build -f services/outbound-service/Dockerfile .
FROM python:3.14-slim
ENV PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1
WORKDIR /app
COPY libs/py-common /build/py-common
COPY integrations/outbound /build/outbound
COPY services/outbound-service/pyproject.toml /build/svc/pyproject.toml
COPY services/outbound-service/app /build/svc/app
RUN --mount=type=cache,target=/root/.cache/pip \
    pip install --disable-pip-version-check --root-user-action=ignore \
      /build/py-common /build/outbound /build/svc && rm -rf /build
RUN useradd --system --no-create-home --shell /usr/sbin/nologin app
USER app
CMD ["python", "-m", "app.main"]
