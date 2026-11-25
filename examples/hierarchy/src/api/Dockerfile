# API: a multi-stage build. The binary is compiled in a golang stage from the
# sources next to this file (the build context is api/); the final stage (the
# image arbor tags) is FROM ${BASE}, the ubuntu parent image.
ARG BASE
FROM golang:1.23 AS build
WORKDIR /src
COPY go.mod main.go ./
RUN CGO_ENABLED=0 go build -o /out/api .

FROM ${BASE}
COPY --from=build /out/api /usr/local/bin/api
EXPOSE 8080
CMD ["api"]
