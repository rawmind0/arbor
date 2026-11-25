# App layer: arbor passes --build-arg BASE=<owner>/base:<version> (its parent).
# By hand: docker build --build-arg BASE=library/base:1.0.1 -f examples/minimal/src/Dockerfile.app examples/minimal/src
ARG BASE
FROM ${BASE}

ENV APP_ENV=production
CMD ["sh", "-c", "echo app running in ${APP_ENV}"]
