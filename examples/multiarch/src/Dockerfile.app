# App layer: BASE is the golang parent image. Compiles a small binary for the
# platform being built.
ARG BASE
FROM ${BASE}

WORKDIR /src
RUN printf 'package main\nimport("fmt";"runtime")\nfunc main(){fmt.Println("app on",runtime.GOOS,runtime.GOARCH)}\n' > main.go \
    && go build -o /usr/local/bin/app main.go
CMD ["app"]
