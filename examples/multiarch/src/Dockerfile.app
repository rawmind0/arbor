{{ template "base" . }}

{{ define "content" }}
# App layer: FROM golang (inherits the Go runtime). Cross-compiles a small
# binary for the platform being built.
WORKDIR /src
RUN printf 'package main\nimport("fmt";"runtime")\nfunc main(){fmt.Println("app on",runtime.GOOS,runtime.GOARCH)}\n' > main.go \
    && go build -o /usr/local/bin/app main.go
CMD ["app"]
{{ end }}
