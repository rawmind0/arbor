# App: BASE is the python parent image. Its build context is this folder (app/),
# so it copies the Python files next to it; any change in this folder changes
# this image.
ARG BASE
FROM ${BASE}

WORKDIR /app
COPY *.py ./
CMD ["python3", "main.py"]
