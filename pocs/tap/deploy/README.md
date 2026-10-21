# Deploying Tap to Cloud Run + Cloud SQL

A staging deploy of the pilot stack: the `tapd` container on Cloud Run, Postgres on Cloud SQL, secrets in Secret Manager.

**Status of this guide.** The image, the Postgres store, the `host=/cloudsql/…` connection style and graceful shutdown were run and checked locally (see "What was checked"). The `gcloud` commands below use flags confirmed against the CLI's own help, but they have **not been run against a real project** — treat the first deploy as the test, and check current Cloud SQL tiers and pricing before you commit.

## Read this first: what the pilot needs that this repo doesn't have yet

| Constraint | Why | What to do |
| --- | --- | --- |
| **No SMS provider.** | Sign-in codes are only returned in `-dev` mode. A public deploy without `-dev` can't sign anyone in; a public deploy *with* `-dev` lets anyone sign up as any phone number. | Deploy staging with `-dev` and keep it **private** (below). Wire an SMS provider before any public pilot. |
| **Exactly one instance.** | Sign-in code state is held in the server's memory, so a code requested on one instance can't be verified on another. Waiting claims and the event queue are also per-instance (claims check the database too, so they survive a rolling deploy; sign-in does not). | `--min-instances=1 --max-instances=1`. Moving sign-in state into the database removes this limit. |
| **CPU always on.** | The event worker and the handshake janitor are background goroutines; with request-only CPU they stall between requests. | `--no-cpu-throttling`. |
| **Dev attestation.** | Device keys are accepted with `dev:` tokens. | Fine for staging; real App Attest / Play Integrity verification is needed before a public pilot. |

## 0. Variables

```bash
PROJECT=my-project  REGION=us-central1  SVC=tap-staging
REPO=tap  IMAGE=$REGION-docker.pkg.dev/$PROJECT/$REPO/tapd:$(git rev-parse --short HEAD)
SQL=tap-pg  DB=tap  DBUSER=tap
gcloud config set project $PROJECT
gcloud services enable run.googleapis.com sqladmin.googleapis.com secretmanager.googleapis.com \
  artifactregistry.googleapis.com cloudbuild.googleapis.com
```

## 1. Build and push the image

```bash
gcloud artifacts repositories create $REPO --repository-format=docker --location=$REGION
gcloud builds submit pocs/tap --tag $IMAGE      # uses pocs/tap/Dockerfile
```

## 2. Postgres

```bash
gcloud sql instances create $SQL --database-version=POSTGRES_16 --region=$REGION \
  --edition=ENTERPRISE --tier=db-f1-micro          # smallest shared-core tier; check current tiers
gcloud sql databases create $DB --instance=$SQL
DBPASS=$(openssl rand -hex 16)
gcloud sql users create $DBUSER --instance=$SQL --password=$DBPASS
```

Cloud Run reaches Cloud SQL over a Unix socket, so the URL names the socket as `host`:

```
postgres://tap:PASSWORD@/tap?host=/cloudsql/PROJECT:REGION:tap-pg
```

(This form was verified against a local Postgres socket. Migrations apply on start.)

## 3. Secrets

`tapd` refuses to start against a database without `TAP_PHONE_SALT` and `TAP_RECEIPT_KEY`. **They must never change for the life of the database**: the salt finds returning users by phone, the key keeps old receipts verifiable. Back them up.

```bash
go run ./backend/cmd/tapd -gen-secrets > /tmp/tap-secrets.env && . /tmp/tap-secrets.env   # from pocs/tap
mk() { printf %s "$2" | gcloud secrets create "$1" --data-file=-; }
mk tap-phone-salt   "$TAP_PHONE_SALT"
mk tap-receipt-key  "$TAP_RECEIPT_KEY"
mk tap-admin-token  "$(openssl rand -hex 16)"
mk tap-database-url "postgres://$DBUSER:$DBPASS@/$DB?host=/cloudsql/$PROJECT:$REGION:$SQL"
shred -u /tmp/tap-secrets.env

# let the service's account read them and connect to Cloud SQL
SA=$(gcloud iam service-accounts list --filter="displayName:Compute Engine default" --format='value(email)')
for s in tap-phone-salt tap-receipt-key tap-admin-token tap-database-url; do
  gcloud secrets add-iam-policy-binding $s --member=serviceAccount:$SA --role=roles/secretmanager.secretAccessor
done
gcloud projects add-iam-policy-binding $PROJECT --member=serviceAccount:$SA --role=roles/cloudsql.client
```

## 4. Deploy (private staging)

```bash
gcloud run deploy $SVC --image=$IMAGE --region=$REGION --port=8080 \
  --args=-dev \
  --no-allow-unauthenticated \
  --min-instances=1 --max-instances=1 --no-cpu-throttling --timeout=30 \
  --add-cloudsql-instances=$PROJECT:$REGION:$SQL \
  --set-secrets=DATABASE_URL=tap-database-url:latest,TAP_PHONE_SALT=tap-phone-salt:latest,TAP_RECEIPT_KEY=tap-receipt-key:latest,TAP_ADMIN_TOKEN=tap-admin-token:latest
```

`--no-allow-unauthenticated` keeps `-dev` sign-in codes away from the internet. Reach the service through an authenticated proxy:

```bash
gcloud run services proxy $SVC --region=$REGION --port=8080 &     # http://localhost:8080
open http://localhost:8080                                         # the two-phone demo
open http://localhost:8080/admin.html                              # token: gcloud secrets versions access latest --secret=tap-admin-token
```

## 5. Smoke test

With the proxy running, the same scenarios CI runs against a local server run against the deployed one, including a burst:

```bash
go run ./backend/cmd/tapsim -url http://localhost:8080 -burst 100
```

Then check `/admin.html`: the claims just made should show as success rate, failure reasons and channel rows.

## Operating notes

- **Shutdown.** Cloud Run sends SIGTERM and allows 10 s. `tapd` stops accepting requests, lets in-flight claims finish (they wait up to 10 s for a partner), drains the event queue, then exits. A claim still waiting when the container stops gets an `expired` answer; the phone retries.
- **Logs.** `tapd` logs one line per event to stdout (Cloud Logging picks it up). Internal errors are logged server-side and returned to clients as a generic 500.
- **Rolling a new image.** `gcloud run deploy $SVC --image=NEWIMAGE …` — during the swap two revisions briefly overlap; waiting claims find their match through the database, but sign-in codes requested on the old revision won't verify on the new one.
- **Rotating the admin token** is safe (`gcloud secrets versions add`); rotating the salt or receipt key is not.

## What was checked

Run locally for this guide:

- `docker build` of `pocs/tap/Dockerfile` (21 MB image, runs as non-root user 65532, distroless base).
- The container serving the demo in `-dev` mode, and against Postgres with the three secrets and an admin token: full `tapsim` scenarios, `/v1/admin/stats` with the token, clean SIGTERM shutdown, and a refusal to start when the secrets are missing.
- The `host=/cloudsql/…`-style URL against a local Postgres Unix socket (migrations applied, server started).
- Two server instances over one database: a claim on instance 1 and its partner on instance 2 match in well under a second instead of waiting out the 10 s window (`TestMatchAcrossInstances`).
- Every `gcloud` flag above exists in the installed CLI's help. Not verified: that the commands succeed against a real project, IAM role names for your org, and current Cloud SQL tier availability and price.
