# SignalHire — engine + recruiter web app in one container.
#
#   docker build -t signalhire .
#   docker run -p 8710:8710 -v signalhire-data:/data \
#     -e SIGNALHIRE_HASH_SALT=change-me signalhire
#
# Set SIGNALHIRE_WEBHOOK_SECRET and STRIPE_LINK_* to enable real billing.

FROM python:3.13-slim

WORKDIR /app
COPY pyproject.toml README.md ./
COPY signalhire ./signalhire
COPY webapp ./webapp
RUN pip install --no-cache-dir ".[web]"

ENV SIGNALHIRE_DB=/data/signalhire.db
VOLUME /data
EXPOSE 8710

CMD ["uvicorn", "webapp.app:app", "--host", "0.0.0.0", "--port", "8710"]
