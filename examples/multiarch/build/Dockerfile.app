FROM registry.example.com/myorg/golang:1.23.5

# Common labels
LABEL image.name="app"
LABEL image.parent="golang"
LABEL image.version="1.0.0"
LABEL project="multiarch-example"

# Build arguments - including platform info
ARG TARGETPLATFORM
ARG TARGETOS
ARG TARGETARCH
ARG BUILDPLATFORM

# App layer: FROM golang (inherits the Go runtime). Cross-compiles a small
# binary for the platform being built.
WORKDIR /src
RUN printf 'package main\nimport("fmt";"runtime")\nfunc main(){fmt.Println("app on",runtime.GOOS,runtime.GOARCH)}\n' > main.go \
    && go build -o /usr/local/bin/app main.go
CMD ["app"]




