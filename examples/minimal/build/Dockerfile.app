FROM library/base:1.0.0

# Common labels
LABEL image.name="app"
LABEL image.parent="base"
LABEL image.version="1.0.0"
LABEL layer="app"
LABEL project="minimal-example"

# Build arguments - including platform info
ARG TARGETPLATFORM
ARG TARGETOS
ARG TARGETARCH
ARG BUILDPLATFORM

# Environment variables
ENV APP_ENV=production

# App layer — its FROM resolves to the base above; it only adds the run command.
CMD ["sh", "-c", "echo app running in ${APP_ENV}"]




