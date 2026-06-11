{{ template "base" . }}

{{ define "content" }}
# App layer — its FROM resolves to the base above; it only adds the run command.
CMD ["sh", "-c", "echo app running in ${APP_ENV}"]
{{ end }}
