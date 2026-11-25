# Frontend app: BASE is the nodejs parent image. Its build context is this
# folder (frontend/), so it copies static/ with a relative path; any change in
# this folder changes this image (and nothing else).
ARG BASE
FROM ${BASE}

ENV PORT=3000
RUN npm install -g serve
COPY static/ /app/
EXPOSE ${PORT}
CMD ["sh", "-c", "serve -s /app -l ${PORT}"]
